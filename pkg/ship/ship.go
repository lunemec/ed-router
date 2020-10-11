package ship

import (
	"errors"
	"math"
)

type Ship interface {
	Jump(distance float64) (Ship, error)
	JumpRange() float64
	JumpRangeWithRemainingFuel() float64
	SecondsToScoop() float64
}

type ship struct {
	// Info from the game.
	jumpRange float64 // unladen jumpRange
	fuelTank  float64 // how big the fuel tank is in tons

	fuelRemaining           float64
	scoopRate               float64 // max scoopRate in kg/s (module description)
	mass                    float64
	currentMass             float64
	fsdOptimalMass          float64
	guardianFSDBoosterRange float64
	maxFuelPerJump          float64
	linearConstant          float64
	powerConstant           float64
}

type (
	linearConstant float64
	powerConstant  float64
)

var (
	// FSDRating map of fsd rating -> linearConstant
	FSDRating = map[string]linearConstant{
		"A": 12,
		"B": 10,
		"C": 8,
		"D": 10,
		"E": 11,
	}
	// FSDClass map of fsd class -> powerConstant
	FSDClass = map[int]powerConstant{
		2: 2.00,
		3: 2.15,
		4: 2.30,
		5: 2.45,
		6: 2.60,
		7: 2.75,
		8: 2.90,
	}
)

var (
	ErrNotEnoughFuel = errors.New("ship does not have enough fuel for the jump")
)

func New(fuelTank, mass, fsdOptimalMass, maxFuelPerJump, guardianFSDBoosterRange, scoopRate float64, linearConstant linearConstant, powerConstant powerConstant) Ship {
	return ship{
		fuelTank:                fuelTank,
		fuelRemaining:           fuelTank,
		mass:                    mass,
		currentMass:             mass,
		fsdOptimalMass:          fsdOptimalMass,
		guardianFSDBoosterRange: guardianFSDBoosterRange,
		maxFuelPerJump:          maxFuelPerJump,
		scoopRate:               scoopRate,
		linearConstant:          float64(linearConstant),
		powerConstant:           float64(powerConstant),
	}
}

// JumpTo calculates fuel cost of this jump and returns new copy of ship struct
// with calculated fuelRemaining.
// https://elite-dangerous.fandom.com/wiki/Frame_Shift_Drive#Hyperspace_Fuel_Equation
func (s ship) Jump(distance float64) (Ship, error) {
	fuelRequired := s.fuelToJump(distance)
	if s.fuelRemaining < fuelRequired {
		return s, ErrNotEnoughFuel
	}
	s.fuelRemaining -= fuelRequired
	s.currentMass -= fuelRequired // decrease ship mass by fuel spent
	return s, nil
}

func (s ship) fuelToJump(distance float64) float64 {
	distance = distance - s.guardianFSDBoosterRange
	return s.linearConstant * 0.001 * math.Pow((distance*s.currentMass)/s.fsdOptimalMass, s.powerConstant)
}

// JumpRange calculates current jump range based on fuel remaining.
// Does not take into account cargo capacity, assuming 0T of cargo.
func (s ship) JumpRange() float64 {
	return ((s.fsdOptimalMass / s.currentMass) * math.Pow((1000*s.maxFuelPerJump)/s.linearConstant, 1/s.powerConstant)) + s.guardianFSDBoosterRange
}

func (s ship) JumpRangeWithRemainingFuel() float64 {
	return 0
}

// SecondsToScoop calculates how long it will take to
// completely refuel the ship given the tank size, remaining fuel and scoop rate.
func (s ship) SecondsToScoop() float64 {
	return ((s.fuelTank - s.fuelRemaining) * 100) / s.scoopRate
}
