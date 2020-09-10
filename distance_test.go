package main

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
	dist := distance(from, to)
	assert.Equal(t, 3.7416573867739413, dist)
}

func TestManhattanDistance(t *testing.T) {
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
	dist := manhattanDistance(from, to)
	assert.Equal(t, 7.4833147735478835, dist)
}
