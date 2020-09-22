package pather

import (
	"errors"
	"testing"

	"github.com/lunemec/ed-router/pkg/distance"
	"github.com/lunemec/ed-router/pkg/ship"

	"github.com/stretchr/testify/assert"
	"gonum.org/v1/gonum/spatial/r3"
)

type testingSystemsStore struct {
	systems []*System
}

func (t *testingSystemsStore) SystemByID64(id64 int64) (*System, error) {
	for _, s := range t.systems {
		if s.ID64 == id64 {
			return s, nil
		}
	}
	return nil, errors.New("system not found")
}

func (t *testingSystemsStore) SystemByName(name string) (*System, error) {
	for _, s := range t.systems {
		if s.Name == name {
			return s, nil
		}
	}
	return nil, errors.New("system not found")
}
func (t *testingSystemsStore) SystemID64sAround(point r3.Vec, d float64) ([]int64, error) {
	var id64s []int64

	for _, s := range t.systems {
		if distance.Distance(s.Coordinates, point) <= d {
			id64s = append(id64s, s.ID64)
		}
	}
	return id64s, nil
}

// TestSystemPatherHappyPath tests 2 systems directly next to each other
// with 3rd farther away.
func TestSystemPatherHappyPath(t *testing.T) {
	testStore := testingSystemsStore{
		systems: []*System{
			{
				ID64: 1,
				Name: "Sol",
				Coordinates: r3.Vec{
					X: 0,
					Y: 0,
					Z: 0,
				},
			},
			{
				ID64: 2,
				Name: "Sol2",
				Coordinates: r3.Vec{
					X: 1,
					Y: 1,
					Z: 1,
				},
			},
			{
				ID64: 3,
				Name: "Sol3",
				Coordinates: r3.Vec{
					X: 3,
					Y: 3,
					Z: 3,
				},
			},
		},
	}
	s := ship.New(10, 0, 0, ship.FSDRating["A"], ship.FSDClass[5])
	p, err := New(&testStore, s, "Sol", "Sol2")
	assert.NoError(t, err)

	path, cost, found := p.Path()
	assert.True(t, found)
	assert.EqualValues(t, 45, cost)
	assert.Equal(t, 2, len(path))
	assert.Equal(t, testStore.systems[0], path[0])
	assert.Equal(t, testStore.systems[1], path[1])
}

func TestSystemPatherImpossible(t *testing.T) {
	testStore := testingSystemsStore{
		systems: []*System{
			{
				ID64: 1,
				Name: "Sol",
				Coordinates: r3.Vec{
					X: 0,
					Y: 0,
					Z: 0,
				},
			},
			{
				ID64: 2,
				Name: "Sol2",
				Coordinates: r3.Vec{
					X: 100,
					Y: 100,
					Z: 100,
				},
			},
		},
	}

	s := ship.New(10, 0, 0, ship.FSDRating["A"], ship.FSDClass[5])
	p, err := New(&testStore, s, "Sol", "Sol2")
	assert.NoError(t, err)

	_, cost, found := p.Path()
	assert.False(t, found)
	assert.EqualValues(t, 0, cost)
}

// TestSystemPatherNeutron tests 2 systems far enough to be reachable by neutron
// charge, but other systems between without neutron charge.
func TestSystemPatherNeutron(t *testing.T) {
	testStore := testingSystemsStore{
		systems: []*System{
			{
				ID64: 1,
				Name: "Sol",
				Coordinates: r3.Vec{
					X: 0,
					Y: 0,
					Z: 0,
				},
			},
			{
				ID64: 2,
				Name: "Neutron",
				Coordinates: r3.Vec{
					X: 10,
					Y: 0,
					Z: 0,
				},
				Stars: []Star{
					{
						Type:     "Neutron Star",
						Distance: 0,
					},
				},
			},
			{
				ID64: 3,
				Name: "Target",
				Coordinates: r3.Vec{
					X: 50,
					Y: 0,
					Z: 0,
				},
			},
			{
				ID64: 4,
				Name: "Filler1",
				Coordinates: r3.Vec{
					X: 9,
					Y: 0,
					Z: 0,
				},
			},
			{
				ID64: 5,
				Name: "Filler2",
				Coordinates: r3.Vec{
					X: 19,
					Y: 0,
					Z: 0,
				},
			},
			{
				ID64: 6,
				Name: "Filler3",
				Coordinates: r3.Vec{
					X: 29,
					Y: 0,
					Z: 0,
				},
			},
			{
				ID64: 7,
				Name: "Filler4",
				Coordinates: r3.Vec{
					X: 39,
					Y: 0,
					Z: 0,
				},
			},
			{
				ID64: 8,
				Name: "Filler5",
				Coordinates: r3.Vec{
					X: 49,
					Y: 0,
					Z: 0,
				},
			},
		},
	}

	s := ship.New(10, 0, 0, ship.FSDRating["A"], ship.FSDClass[5])
	p, err := New(&testStore, s, "Sol", "Target")
	assert.NoError(t, err)

	path, cost, found := p.Path()
	assert.True(t, found)

	assert.EqualValues(t, 100, cost)
	assert.Equal(t, 3, len(path))
	assert.Equal(t, testStore.systems[0], path[0])
	assert.Equal(t, testStore.systems[1], path[1])
	assert.Equal(t, testStore.systems[2], path[2])
}
