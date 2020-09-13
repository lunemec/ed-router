package route

import (
	"testing"

	"github.com/beefsack/go-astar"
	"github.com/stretchr/testify/assert"
	"gonum.org/v1/gonum/spatial/r3"
)

// TestSystemPatherHappyPath tests 2 systems directly next to each other
// with 3rd farther away.
func TestSystemPatherHappyPath(t *testing.T) {
	var systems systems
	systems = append(systems,
		&system{
			ID64: 1,
			Name: "Sol",
			Coordinates: r3.Vec{
				X: 0,
				Y: 0,
				Z: 0,
			},
			shipJumpRange: 10,
			allSystems:    &systems,
		},
		&system{
			ID64: 2,
			Name: "Sol2",
			Coordinates: r3.Vec{
				X: 1,
				Y: 1,
				Z: 1,
			},
			shipJumpRange: 10,
			allSystems:    &systems,
		},
		&system{
			ID64: 3,
			Name: "Sol3",
			Coordinates: r3.Vec{
				X: 3,
				Y: 3,
				Z: 3,
			},
			shipJumpRange: 10,
			allSystems:    &systems,
		},
	)

	path, cost, found := astar.Path(systems[0], systems[1])
	assert.True(t, found)
	assert.EqualValues(t, 10, cost)
	assert.Equal(t, 2, len(path))
	assert.Equal(t, systems[0], path[1])
	assert.Equal(t, systems[1], path[0])
}

func TestSystemPatherImpossible(t *testing.T) {
	var systems systems
	systems = append(systems,
		&system{
			ID64: 1,
			Name: "Sol",
			Coordinates: r3.Vec{
				X: 0,
				Y: 0,
				Z: 0,
			},
			shipJumpRange: 10,
			allSystems:    &systems,
		},
		&system{
			ID64: 2,
			Name: "Sol2",
			Coordinates: r3.Vec{
				X: 100,
				Y: 100,
				Z: 100,
			},
			shipJumpRange: 10,
			allSystems:    &systems,
		},
	)

	_, cost, found := astar.Path(systems[0], systems[1])
	assert.False(t, found)
	assert.EqualValues(t, 0, cost)
}

// TestSystemPatherNeutron tests 2 systems far enough to be reachable by neutron
// charge, but other systems between without neutron charge.
func TestSystemPatherNeutron(t *testing.T) {
	var systems systems
	systems = append(systems,
		&system{
			ID64: 1,
			Name: "Sol",
			Coordinates: r3.Vec{
				X: 0,
				Y: 0,
				Z: 0,
			},
			shipJumpRange: 10,
			allSystems:    &systems,
		},
		&system{
			ID64: 2,
			Name: "Neutron",
			Coordinates: r3.Vec{
				X: 10,
				Y: 0,
				Z: 0,
			},
			stars: []star{
				{
					Type:     "Neutron Star",
					Distance: 0,
				},
			},
			shipJumpRange: 10,
			allSystems:    &systems,
		},
		&system{
			ID64: 3,
			Name: "Target",
			Coordinates: r3.Vec{
				X: 50,
				Y: 0,
				Z: 0,
			},
			shipJumpRange: 10,
			allSystems:    &systems,
		},
		&system{
			ID64: 4,
			Name: "Filler1",
			Coordinates: r3.Vec{
				X: 9,
				Y: 0,
				Z: 0,
			},
			shipJumpRange: 10,
			allSystems:    &systems,
		},
		&system{
			ID64: 5,
			Name: "Filler2",
			Coordinates: r3.Vec{
				X: 19,
				Y: 0,
				Z: 0,
			},
			shipJumpRange: 10,
			allSystems:    &systems,
		},
		&system{
			ID64: 6,
			Name: "Filler3",
			Coordinates: r3.Vec{
				X: 29,
				Y: 0,
				Z: 0,
			},
			shipJumpRange: 10,
			allSystems:    &systems,
		},
		&system{
			ID64: 7,
			Name: "Filler4",
			Coordinates: r3.Vec{
				X: 39,
				Y: 0,
				Z: 0,
			},
			shipJumpRange: 10,
			allSystems:    &systems,
		},
		&system{
			ID64: 8,
			Name: "Filler5",
			Coordinates: r3.Vec{
				X: 49,
				Y: 0,
				Z: 0,
			},
			shipJumpRange: 10,
			allSystems:    &systems,
		},
	)

	path, cost, found := astar.Path(systems[0], systems[2])
	assert.True(t, found)
	assert.EqualValues(t, 30, cost)
	assert.Equal(t, 3, len(path))
	assert.Equal(t, systems[0], path[2])
	assert.Equal(t, systems[1], path[1])
	assert.Equal(t, systems[2], path[0])
}
