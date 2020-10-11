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

type Systems []*System
type System struct {
	Coordinates r3.Vec // coordinates in the dump mean distance in LY from Sol, which is 0,0,0.
	ID64        int64
	Name        string
	Stars       []Star

	ChargeAt *Star
	RefuelAt *Star

	pather  *pather
	leadsTo []Jump
	ship    ship.Ship
}

type Star struct {
	ID64     int64
	Name     string
	Type     string  // TODO TYPE
	Distance float64 // distance to arrival
}

func (s *System) PathNeighbors() []astar.Pather {
	var (
		neighbors []astar.Pather
	)

	closestChargeable, chargeMultiplier := s.Chargeable()
	maxRange := s.ship.JumpRange() * chargeMultiplier

	s.ChargeAt = closestChargeable

	systemsInRange, err := s.pather.systemsInRangeOf(s, maxRange)
	if err != nil {
		fmt.Printf("ERROR: %+v \n", err)
		return neighbors
	}

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

	chargeStar, _ := toSystem.Chargeable()
	if chargeStar != nil {
		cost -= 75.0
		// Increase cost when neutron star is far away from jump-in point.
		cost += (chargeStar.Distance / 10.0)
	}
	scoopableStar := toSystem.Scoopable()
	if scoopableStar != nil {
		cost -= 25.0
		// It is not worth it to fly very far in supercruise for refuel.
		// TODO: reasoning + calculation.
		cost += scoopableStar.Distance
	}
	return cost
}

// PathEstimatedCost estimates cost in LY.
func (s *System) PathEstimatedCost(to astar.Pather) float64 {
	toSystem := to.(*System)
	return distance.Distance(s.Coordinates, toSystem.Coordinates)
}

func (s *System) Chargeable() (*Star, float64) {
	var (
		closestChargeable *Star
		multiplier        float64 = 1
	)

	if s.Stars == nil {
		return closestChargeable, multiplier
	}
	for _, star := range s.Stars {
		// White dwarfs are not checked since they are considered "not worth it".
		if star.Type == "Neutron Star" {
			if closestChargeable == nil {
				closestChargeable = &star
				multiplier = 4.0
				continue
			}
			if star.Distance < closestChargeable.Distance {
				closestChargeable = &star
				multiplier = 4.0
			}
		}
	}

	return closestChargeable, multiplier
}

func (s *System) Scoopable() *Star {
	// KGBFOAM stars are scoopable.
	var closestScoopable *Star
	for _, star := range s.Stars {
		switch star.Type {
		case
			"A (Blue-White super giant) Star",
			"A (Blue-White) Star",
			"B (Blue-White super giant) Star",
			"B (Blue-White) Star",
			"F (White super giant) Star",
			"F (White) Star",
			"G (White-Yellow super giant) Star",
			"G (White-Yellow) Star",
			"K (Yellow-Orange giant) Star",
			"K (Yellow-Orange) Star",
			"M (Red dwarf) Star",
			"M (Red giant) Star",
			"M (Red super giant) Star",
			"O (Blue-White) Star":

			if closestScoopable == nil {
				closestScoopable = &star
			}
			if star.Distance < closestScoopable.Distance {
				closestScoopable = &star
			}
		}

	}
	return closestScoopable
}
