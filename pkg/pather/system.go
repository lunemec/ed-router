package pather

import (
	"fmt"

	"github.com/lunemec/ed-router/pkg/distance"
	"github.com/lunemec/ed-router/pkg/ship"

	"github.com/beefsack/go-astar"
	"gonum.org/v1/gonum/spatial/r3"
)

type Jump struct {
	from *System
	to   *System
}

type System struct {
	Coordinates r3.Vec // coordinates in the dump mean distance in LY from Sol, which is 0,0,0.
	ID64        uint64
	Neutron     bool
	Scoopable   bool

	pather  *pather
	leadsTo []Jump
	ship    ship.Ship
}

func (s *System) PathNeighbors() []astar.Pather {
	var (
		neighbors []astar.Pather
	)

	maxRange := s.ship.JumpRange()
	if s.Neutron {
		maxRange *= 4
	}

	systemsInRange, err := s.pather.systemsInRangeOf(s, maxRange)
	if err != nil {
		fmt.Printf("ERROR: %+v \n", err)
		return neighbors
	}
	fmt.Printf("inRange: %+v\n", systemsInRange)

	for _, otherSystem := range systemsInRange {
		if s.ID64 == otherSystem.ID64 {
			continue
		}
		dist := distance.Distance(s.Coordinates, otherSystem.Coordinates)
		if dist <= maxRange {
			newShip, err := s.ship.Jump(dist)
			if err != nil {
				continue
			}
			otherSystem.ship = newShip
			s.leadsTo = append(s.leadsTo, Jump{
				from: s,
				to:   otherSystem,
			})
		}
	}
	for _, canJumpTo := range s.leadsTo {
		neighbors = append(neighbors, canJumpTo.to)
	}
	return neighbors
}

// PathNeighborCost is cost of this neighbor in seconds.
//
func (s *System) PathNeighborCost(to astar.Pather) float64 {
	// We reduce cost by 75 for neutron and by 25 by scoopable.
	// Ideal case is system with neutron + scoopable at 0 Ls, which is impossible.
	cost := 101.0

	toSystem := to.(*System)

	if toSystem.Neutron {
		cost -= 75.0
	}
	if toSystem.Scoopable {
		cost -= 25.0
	}
	return cost
}

// PathEstimatedCost estimates cost in LY.
func (s *System) PathEstimatedCost(to astar.Pather) float64 {
	toSystem := to.(*System)
	return distance.Distance(s.Coordinates, toSystem.Coordinates)
}
