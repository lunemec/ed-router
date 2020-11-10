package pather

import (
	"math"

	"gonum.org/v1/gonum/spatial/r3"
)

// isInSphere checks if point p is within a sphere with center c and radius r.
//   (𝑥−𝑐𝑥)^2+(𝑦−𝑐𝑦)^2+(𝑧−𝑐𝑧)^2 < 𝑟^2
func isInSphere(p r3.Vec, c r3.Vec, r float64) bool {
	return math.Pow(p.X-c.X, 2)+math.Pow(p.Y-c.Y, 2)+math.Pow(p.Z-c.Z, 2) < math.Pow(r, 2)
}
