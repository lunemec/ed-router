package route

import (
	"math"

	"github.com/beefsack/go-astar"
	"gonum.org/v1/gonum/spatial/r3"
)

const maxCost = math.MaxFloat64

type jump struct {
	from *system
	to   *system
	cost float64
}

type systems []*system
type system struct {
	Coordinates r3.Vec // coordinates in the dump mean distance in LY from Sol, which is 0,0,0.
	ID64        int64
	Name        string
	stars       []star
	leadsTo     []jump

	ship       ship
	allSystems *systems
}

type star struct {
	ID64     int64
	Name     string
	Type     string  // TODO TYPE
	Distance float64 // distance to arrival
}

const (
	secondsToJump        float64 = 10
	secondsToSupercharge float64 = 10
)

func (s *system) PathNeighbors() []astar.Pather {
	var (
		neighbors []astar.Pather
	)

	_, chargeMultiplier := s.Chargeable()
	maxRange := s.shipJumpRange * chargeMultiplier
	for _, otherSystem := range *s.allSystems {
		// There can be multiple systems with identical id64 and name, but not coordinates.
		if s.Coordinates == otherSystem.Coordinates {
			continue
		}
		if distance(s.Coordinates, otherSystem.Coordinates) <= maxRange {
			s.leadsTo = append(s.leadsTo, jump{
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
func (s *system) PathNeighborCost(to astar.Pather) float64 {
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
func (s *system) PathEstimatedCost(to astar.Pather) float64 {
	toSystem := to.(*system)
	dist := distance(s.Coordinates, toSystem.Coordinates)
	jumps := dist / s.shipJumpRange
	return jumps * secondsToJump
}

func (s *system) Chargeable() (*star, float64) {
	var (
		closestChargeable *star
		multiplier        float64 = 1
	)

	if s.stars == nil {
		return closestChargeable, multiplier
	}
	for _, star := range s.stars {
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

func (s *system) Scoopable() *star {
	// KGBFOAM stars are scoopable.
	var closestScoopable *star
	for _, star := range s.stars {
		if star.Type == "" {

		}
	}
	return closestScoopable
}
