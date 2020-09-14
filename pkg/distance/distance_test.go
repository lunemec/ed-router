package distance

import (
	"testing"

	"gonum.org/v1/gonum/spatial/r3"

	"github.com/stretchr/testify/assert"
)

func TestDistance(t *testing.T) {
	from := r3.Vec{
		X: 0,
		Y: 0,
		Z: 0,
	}
	to := r3.Vec{
		X: 1,
		Y: 2,
		Z: 3,
	}
	dist := Distance(from, to)
	assert.Equal(t, 3.7416573867739413, dist)
}
