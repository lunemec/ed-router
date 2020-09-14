package pather

import (
	"github.com/lunemec/ed-router/pkg/distance"

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

	pather  *pather
	leadsTo []Jump
	ship    Ship
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

	_, chargeMultiplier := s.Chargeable()
	maxRange := s.ship.JumpRange() * chargeMultiplier

	systemsInRange, err := s.pather.systemsInRangeOf(s, maxRange)
	if err != nil {
		panic(err)
	}

	for _, otherSystem := range systemsInRange {
		if s.ID64 == otherSystem.ID64 {
			continue
		}
		if distance.Distance(s.Coordinates, otherSystem.Coordinates) <= maxRange {
			otherSystem.ship = s.ship.JumpTo() // TODO
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
	secondsCost := secondsToJump
	chargeStar, _ := s.Chargeable()
	if chargeStar != nil {
		secondsCost = secondsToJump + secondsToSupercharge
	}
	return secondsCost
}

// PathEstimatedCost estimates cost in seconds.
// Estimated cost would be:
//   ((range from, to) / normal ship range) * seconds to jump to next system
func (s *System) PathEstimatedCost(to astar.Pather) float64 {
	toSystem := to.(*System)
	dist := distance.Distance(s.Coordinates, toSystem.Coordinates)
	jumps := dist / s.ship.JumpRange()
	return jumps * secondsToJump
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
		if star.Type == "" {

		}
	}
	return closestScoopable
}
