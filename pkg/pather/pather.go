package pather

import (
	"fmt"
	"math"
	"sync"

	"github.com/lunemec/ed-router/pkg/db/boltdb"
	"github.com/lunemec/ed-router/pkg/distance"
	"github.com/lunemec/ed-router/pkg/ship"
	"github.com/vbauerster/mpb/v5"

	"github.com/beefsack/go-astar"
	"github.com/dhconnelly/rtreego"
	"github.com/pkg/errors"
	"gonum.org/v1/gonum/spatial/r3"
)

const maxCost = math.MaxFloat64

const (
	secondsToJump        float64 = 45
	secondsToSupercharge float64 = 10
)

type Pather interface {
	From() *System
	To() *System
	Distance() float64
	Path() ([]*System, float64, bool)
}

type pather struct {
	systems map[uint64]*System
	store   *boltdb.DB
	rtree   *rtreego.Rtree

	from           *System
	to             *System
	distance       float64
	systemsChecked int

	pb  *mpb.Progress
	bar *mpb.Bar
}

func New(store *boltdb.DB, ship ship.Ship, fromName, toName string) (*pather, error) {
	var p = pather{
		systems: make(map[uint64]*System),
		store:   store,
	}

	from, err := p.systemByName(fromName)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to find system FROM: %s", fromName)
	}
	from.ship = ship
	p.from = from

	to, err := p.systemByName(toName)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to find system TO: %s", toName)
	}
	to.ship = ship
	p.to = to
	p.distance = distance.Distance(from.Coordinates, to.Coordinates)

	minX := from.Coordinates.X
	maxX := to.Coordinates.X
	minY := from.Coordinates.Y
	maxY := to.Coordinates.Y
	minZ := from.Coordinates.Z
	maxZ := to.Coordinates.Z

	if minX > maxX {
		minX, maxX = maxX, minX
	}
	if minY > maxY {
		minY, maxY = maxY, minY
	}
	if minZ > maxZ {
		minZ, maxZ = maxZ, minZ
	}

	var wg sync.WaitGroup
	systemsChan := make(chan boltdb.System)
	p.rtree = rtreego.NewTree(3, 25, 50)

	wg.Add(1)
	go func() {
		defer wg.Done()
		err := p.store.PointsWithinXYZBucketsChan(minX, maxX, minY, maxY, minZ, maxZ, systemsChan)
		if err != nil {
			fmt.Printf("error loading systems within %+v\n", err)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		p.rtree.Insert(p.from)
		p.rtree.Insert(p.to)

		for system := range systemsChan {
			coordinates := r3.Vec{X: system.X, Y: system.Y, Z: system.Z}
			if p.isInCylinder(coordinates) {
				p.rtree.Insert(&System{
					Coordinates: coordinates,
					ID64:        system.ID64,
					Neutron:     system.IsNeutron,
					Scoopable:   system.IsScoopable,
					pather:      &p,
				})
			}
		}
	}()

	wg.Wait()

	// pb := mpb.New(
	// 	mpb.WithRefreshRate(180 * time.Millisecond),
	// )
	// p.pb = pb
	// bar := pb.AddBar(int64(p.distance),
	// 	mpb.BarStyle("[=>-|"),
	// 	mpb.PrependDecorators(
	// 		decor.CountersNoUnit("% d / % d"),
	// 	),
	// 	mpb.AppendDecorators(
	// 		decor.EwmaETA(decor.ET_STYLE_GO, 90),
	// 		decor.Name(" ] "),
	// 	))
	// p.bar = bar
	return &p, nil
}

func (p *pather) Stats() int {
	return p.systemsChecked
}

func (p *pather) From() *System {
	return p.from
}

func (p *pather) To() *System {
	return p.to
}

func (p *pather) Distance() float64 {
	return p.distance
}

func (p *pather) Path() ([]*System, float64, bool) {
	path, cost, found := astar.Path(p.from, p.to)
	if !found {
		return nil, 0, false
	}
	var systems []*System
	for i := len(path) - 1; i >= 0; i-- {
		systems = append(systems, path[i].(*System))
	}
	return systems, cost, true
}

func (p *pather) isInCylinder(point r3.Vec) bool {
	return isInCylinder(p.from.Coordinates, p.to.Coordinates, p.from.ship.JumpRange()*10, point)
}

func (p *pather) systemByName(name string) (*System, error) {
	dbS, err := p.store.SystemByName(name)
	if err != nil {
		return nil, err
	}
	s := &System{
		Coordinates: r3.Vec{
			X: dbS.Coordinates.X,
			Y: dbS.Coordinates.Y,
			Z: dbS.Coordinates.Z,
		},
		ID64:      dbS.ID64,
		Neutron:   boltdb.NeutronInRange(dbS.Bodies),
		Scoopable: boltdb.ScoopableInRange(dbS.Bodies),
	}
	sc, ok := p.systems[s.ID64]
	if !ok {
		s.pather = p
		p.systems[s.ID64] = s
		sc = s
	}
	return sc, nil
}

func (p *pather) systemsInRangeOf(s *System, distance float64) ([]*System, error) {
	point := rtreego.Point{
		s.Coordinates.X - distance,
		s.Coordinates.Y - distance,
		s.Coordinates.Z - distance,
	}
	distanceX2 := distance * 2
	bb, _ := rtreego.NewRect(point, []float64{distanceX2, distanceX2, distanceX2})
	results := p.rtree.SearchIntersect(bb)

	var out []*System
	outerSphereRadius := distance
	innerSphereRadius := outerSphereRadius * 0.90 // 90% of outerSphereRadius
	// If the target system is within reach, do not filter out the innerSphere
	// - set its radius to 0.
	// This is to avoid filtering out the target system.
	if isInSphere(p.to.Coordinates, s.Coordinates, outerSphereRadius) {
		innerSphereRadius = 0
	}
	if isInSphere(p.from.Coordinates, s.Coordinates, outerSphereRadius) {
		innerSphereRadius = 0
	}
	for _, res := range results {
		target := res.(*System)
		// If target system is not in range, skip it. (we searched for cube).
		if !isInSphere(target.Coordinates, s.Coordinates, outerSphereRadius) {
			continue
		}
		// If the target system is inside of the sphere of % radius of the outer
		// sphere, and is not neutron, skip.
		if isInSphere(target.Coordinates, s.Coordinates, innerSphereRadius) && !target.Neutron {
			continue
		}
		out = append(out, target)
	}

	return out, nil
}

func (p *pather) systemsInRangeOf2(s *System, distance float64) ([]*System, error) {
	minX := s.Coordinates.X - distance
	maxX := s.Coordinates.X + distance
	minY := s.Coordinates.Y - distance
	maxY := s.Coordinates.Y + distance
	minZ := s.Coordinates.Z - distance
	maxZ := s.Coordinates.Z + distance
	dbSystems, err := p.store.PointsWithinXYZBuckets(minX, maxX, minY, maxY, minZ, maxZ)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to get systems in range of %d", s.ID64)
	}
	var filteredDbSystems []boltdb.System

	outerSphereRadius := distance
	innerSphereRadius := outerSphereRadius * 0.99 // 90% of outerSphereRadius
	// If the target system is within reach, do not filter out the innerSphere
	// - set its radius to 0.
	// This is to avoid filtering out the target system.
	if isInSphere(p.to.Coordinates, s.Coordinates, outerSphereRadius) {
		innerSphereRadius = 0
	}
	for _, dbSystem := range dbSystems {
		p := r3.Vec{X: dbSystem.X, Y: dbSystem.Y, Z: dbSystem.Z}
		// Is actually in the distance (we searched for cube).
		if isInSphere(p, s.Coordinates, outerSphereRadius) {
			// Remove most of the center of the sphere, we want only systems within
			// some % of the max distance.
			if isInSphere(p, s.Coordinates, innerSphereRadius) {
				continue
			}
			filteredDbSystems = append(filteredDbSystems, dbSystem)
		}
	}

	var systems []*System
	for _, dbSystem := range filteredDbSystems {
		sc, ok := p.systems[dbSystem.ID64]
		if ok {
			systems = append(systems, sc)
		} else {
			s := &System{
				pather:      p,
				Coordinates: r3.Vec{X: dbSystem.X, Y: dbSystem.Y, Z: dbSystem.Z},
				ID64:        dbSystem.ID64,
				Neutron:     dbSystem.IsNeutron,
				Scoopable:   dbSystem.IsScoopable,
			}
			p.systems[dbSystem.ID64] = s
			systems = append(systems, s)
		}
	}
	return systems, nil
}
