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

type fakeShip struct {
	jumpRange                  float64
	jumpRangeWithRemainingFuel float64
	secondsToScoop             float64
}

func (s fakeShip) Jump(dist float64) (ship.Ship, error) {
	return s, nil
}

func (s fakeShip) JumpRange() float64 {
	return s.jumpRange
}

func (s fakeShip) JumpRangeWithRemainingFuel() float64 {
	return s.jumpRangeWithRemainingFuel
}
func (s fakeShip) SecondsToScoop() float64 {
	return s.secondsToScoop
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
	s := fakeShip{
		jumpRange:                  10,
		jumpRangeWithRemainingFuel: 10,
	}
	p, err := New(&testStore, s, "Sol", "Sol2")
	assert.NoError(t, err)

	path, cost, found := p.Path()
	assert.True(t, found)
	assert.EqualValues(t, 101, cost)
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

	s := fakeShip{
		jumpRange:                  10,
		jumpRangeWithRemainingFuel: 10,
	}
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

	s := fakeShip{
		jumpRange:                  10,
		jumpRangeWithRemainingFuel: 10,
	}
	p, err := New(&testStore, s, "Sol", "Target")
	assert.NoError(t, err)

	path, cost, found := p.Path()
	assert.True(t, found)

	assert.EqualValues(t, 127, cost)
	assert.Equal(t, 3, len(path))
	assert.Equal(t, testStore.systems[0], path[0])
	assert.Equal(t, testStore.systems[1], path[1])
	assert.Equal(t, testStore.systems[2], path[2])
}

// TestSystemPatherNeutronDistant tests 2 systems far enough to be reachable by neutron
// charge, but neutron is distant from jumpin point, so it will be more worth it to choose
// more jumps.
func TestSystemPatherNeutronDistant(t *testing.T) {
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
						Distance: 4790,
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

	s := fakeShip{
		jumpRange:                  10,
		jumpRangeWithRemainingFuel: 10,
	}
	p, err := New(&testStore, s, "Sol", "Target")
	assert.NoError(t, err)

	path, cost, found := p.Path()
	assert.True(t, found)

	assert.EqualValues(t, 606, cost)
	assert.Equal(t, 7, len(path))
	assert.Equal(t, testStore.systems[0], path[0])
	assert.Equal(t, testStore.systems[3], path[1])
	assert.Equal(t, testStore.systems[4], path[2])
	assert.Equal(t, testStore.systems[5], path[3])
	assert.Equal(t, testStore.systems[6], path[4])
	assert.Equal(t, testStore.systems[7], path[5])
	assert.Equal(t, testStore.systems[2], path[6])
}

func BenchmarkNeutron(b *testing.B) {
	for n := 0; n < b.N; n++ {
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

		s := fakeShip{
			jumpRange:                  10,
			jumpRangeWithRemainingFuel: 10,
		}
		p, err := New(&testStore, s, "Sol", "Target")
		assert.NoError(b, err)

		path, cost, found := p.Path()
		assert.True(b, found)

		assert.EqualValues(b, 127, cost)
		assert.Equal(b, 3, len(path))
		assert.Equal(b, testStore.systems[0], path[0])
		assert.Equal(b, testStore.systems[1], path[1])
		assert.Equal(b, testStore.systems[2], path[2])
	}
}
