package pather

import (
	"gonum.org/v1/gonum/spatial/r3"
)

// isInCylinder is filter that checks if any point is inside cylinder between start/end points with radius.
//
// https://stackoverflow.com/questions/47932955/how-to-check-if-a-3d-point-is-inside-a-cylinder/47933302#47933302
func isInCylinder(start, end r3.Vec, radius float64, point r3.Vec) bool {
	vec := end.Sub(start)
	comp := radius * r3.Norm(vec)
	return point.Sub(start).Dot(vec) >= 0 && point.Sub(end).Dot(vec) <= 0 &&
		r3.Norm(point.Sub(start).Cross(vec)) <= comp
}
