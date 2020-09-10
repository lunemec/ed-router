package main

import (
	"testing"

	"gonum.org/v1/gonum/spatial/r3"

	"github.com/stretchr/testify/assert"
)

func TestIsInCylinder(t *testing.T) {
	from := r3.Vec{
		X: 0,
		Y: 0,
		Z: 0,
	}
	to := r3.Vec{
		X: 10,
		Y: 10,
		Z: 10,
	}
	point := r3.Vec{
		X: 1,
		Y: 1,
		Z: 1,
	}
	inCylinder := isInCylinder(from, to, 1, point)
	assert.True(t, inCylinder)
}

func TestIsNotInCylinder(t *testing.T) {
	from := r3.Vec{
		X: 0,
		Y: 0,
		Z: 0,
	}
	to := r3.Vec{
		X: 10,
		Y: 10,
		Z: 10,
	}
	point := r3.Vec{
		X: 11,
		Y: 11,
		Z: 11,
	}
	inCylinder := isInCylinder(from, to, 1, point)
	assert.False(t, inCylinder)
}
