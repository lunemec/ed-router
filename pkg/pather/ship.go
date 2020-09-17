package pather

import "math"

type Ship struct {
	// Info from the game.
	jumpRange float64 // unladen jumpRange
	fuelTank  float64 // how big the fuel tank is in tons

	fuelRemaining  float64 // in kg
	scoopRate      float64 // max scoopRate in kg/s (module description)
	fsdBooster     bool    // has Guardian FSD Booster
	mass           float64
	fsdOptimalMass float64
	linearConstant float64
	powerConstant  float64
}

var (
	// linearConstant map of FSD rating -> linearConstant
	linearConstant = map[string]float64{
		"A": 12,
		"B": 10,
		"C": 8,
		"D": 10,
		"E": 11,
	}
	// FSD class -> powerConstant
	powerConstant = map[int]float64{
		2: 2.00,
		3: 2.15,
		4: 2.30,
		5: 2.45,
		6: 2.60,
		7: 2.75,
		8: 2.90,
	}
)

func NewShip(jumpRange, fuelTank, scoopRate, linearConstant, powerConstant float64, fsdBooster bool) Ship {
	return Ship{
		jumpRange:      jumpRange,
		fuelTank:       fuelTank,
		fuelRemaining:  fuelTank * 100,
		scoopRate:      scoopRate,
		fsdBooster:     fsdBooster,
		linearConstant: linearConstant,
		powerConstant:  powerConstant,
	}
}

// JumpTo calculates fuel cost of this jump and returns new copy of ship struct
// with calculated fuelRemaining.
// https://elite-dangerous.fandom.com/wiki/Frame_Shift_Drive#Hyperspace_Fuel_Equation
func (s Ship) Jump(distance float64) Ship {
	fuelConsumed := s.linearConstant * 0.001 * math.Pow((distance*s.mass)/s.fsdOptimalMass, s.powerConstant)
	s.fuelRemaining -= fuelConsumed
	return s
}

// JumpRange calculates current jump range based on fuel remaining.
// Does not take into account cargo capacity, assuming 0T of cargo.
func (s Ship) JumpRange() float64 {
	return s.jumpRange // TODO
}

// SecondsToScoop calculates how long it will take to
// completely refuel the ship given the tank size, remaining fuel and scoop rate.
func (s Ship) SecondsToScoop() float64 {
	return ((s.fuelTank * 100) - s.fuelRemaining) / s.scoopRate
}
