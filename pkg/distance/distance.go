package distance

import (
	"math"

	"gonum.org/v1/gonum/spatial/r3"
)

// Distance calculates distance between two coordinates using Pythagorean theorem.
func Distance(from, to r3.Vec) float64 {
	return math.Sqrt(math.Pow(to.X-from.X, 2) + math.Pow(to.Y-from.Y, 2) + math.Pow(to.Z-from.Z, 2))
}
