package main

import (
	"math"

	"gonum.org/v1/gonum/spatial/r3"
)

// distance calculates distance between two coordinates using Pythagorean theorem.
func distance(from, to r3.Vec) float64 {
	return math.Sqrt(math.Pow(to.X-from.X, 2) + math.Pow(to.Y-from.Y, 2) + math.Pow(to.Z-from.Z, 2))
}

// manhattanDistance calculates manhattan distance between two coordinates.
func manhattanDistance(from, to r3.Vec) float64 {
	return r3.Norm(from.Sub(to)) + r3.Norm(to.Sub(from))
}
