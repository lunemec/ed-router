package pather

type Ship struct {
	// Info from the game.
	jumpRange float64 // unladen jumpRange
	fuelTank  float64 // how big the fuel tank is in tons

	fuelRemaining float64 // in kg
	scoopRate     float64 // max scoopRate in kg/s (module description)
}

// JumpTo calculates fuel cost of this jump and returns new copy of ship struct
// with calculated fuelRemaining.
func (s Ship) JumpTo() Ship {
	return s // TODO
}

// JumpRange calculates current jump range based on fuel remaining.
// Does not take into account cargo capacity, assuming 0T of cargo.
func (s Ship) JumpRange() float64 {
	return 70 //s.jumpRange // TODO
}

// SecondsToScoop calculates how long it will take to
// completely refuel the ship given the tank size, remaining fuel and scoop rate.
func (s Ship) SecondsToScoop() float64 {
	return ((s.fuelTank * 100) - s.fuelRemaining) / s.scoopRate
}
