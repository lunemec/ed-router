package pather

import (
	"math"

	"github.com/lunemec/ed-router/pkg/db/boltdb"
	"github.com/lunemec/ed-router/pkg/distance"
	"github.com/lunemec/ed-router/pkg/ship"

	"github.com/beefsack/go-astar"
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

	from     *System
	to       *System
	distance float64
}

func New(store *boltdb.DB, ship ship.Ship, fromName, toName string) (Pather, error) {
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

	return &p, nil
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
	return isInCylinder(p.from.Coordinates, p.to.Coordinates, p.from.ship.JumpRange(), point)
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
	minX := s.Coordinates.X - distance
	maxX := s.Coordinates.X + distance
	minY := s.Coordinates.Y - distance
	maxY := s.Coordinates.Y + distance
	minZ := s.Coordinates.Z - distance
	maxZ := s.Coordinates.Z + distance
	dbSystems, err := p.store.PointsWithin(minX, maxX, minY, maxY, minZ, maxZ)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to get systems in range of %d", s.ID64)
	}

	var systems []*System
	for _, dbSystem := range dbSystems {
		systems = append(systems, &System{
			Coordinates: r3.Vec{X: dbSystem.X, Y: dbSystem.Y, Z: dbSystem.Z},
			ID64:        dbSystem.ID64,
			Neutron:     dbSystem.IsNeutron,
			Scoopable:   dbSystem.IsScoopable,
		})
	}
	return systems, nil
}
