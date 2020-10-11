package pather

import (
	"math"

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

// SystemsStore is any type that implements methods required for router to work.
type SystemsStore interface {
	SystemByID64(id64 int64) (*System, error)
	SystemByName(name string) (*System, error)
	SystemID64sAround(point r3.Vec, distance float64) ([]int64, error)
}

type Pather interface {
	From() *System
	To() *System
	Distance() float64
	Path() ([]*System, float64, bool)
}

type pather struct {
	systems map[int64]*System
	store   SystemsStore

	from     *System
	to       *System
	distance float64
}

func New(store SystemsStore, ship ship.Ship, fromName, toName string) (Pather, error) {
	var p = pather{
		systems: make(map[int64]*System),
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
	path, cost, found := astar.Path(p.from, p.to, p.cleanup)
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

func (p *pather) cleanup(node astar.Pather) {
	system := node.(*System)
	delete(p.systems, system.ID64)
}

func (p *pather) systemByID64(id64 int64) (*System, error) {
	sc, ok := p.systems[id64]
	if ok {
		return sc, nil
	}

	s, err := p.store.SystemByID64(id64)
	if err != nil {
		return nil, err
	}

	s.pather = p
	p.systems[id64] = s
	return s, nil
}

func (p *pather) systemByName(name string) (*System, error) {
	s, err := p.store.SystemByName(name)
	if err != nil {
		return nil, err
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
	id64s, err := p.store.SystemID64sAround(s.Coordinates, distance)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to get systems in range of %s", s.Name)
	}

	var systems []*System
	for _, id64 := range id64s {
		s, err := p.systemByID64(id64)
		if err != nil {
			return nil, errors.Wrapf(err, "unable to get system by id64: %d", id64)
		}
		systems = append(systems, s)
	}
	return systems, nil
}
