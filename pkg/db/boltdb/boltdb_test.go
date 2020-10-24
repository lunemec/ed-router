package boltdb

import (
	"fmt"
	"os"
	"testing"

	"github.com/lunemec/ed-router/pkg/models/dump"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	"gonum.org/v1/gonum/spatial/r3"
)

type BoltDBTestSuite struct {
	suite.Suite

	db *DB
}

var (
	testIndexFile  = "testindex.db"
	testGalaxyFile = "testgalaxy.db"
)

func (t *BoltDBTestSuite) SetupTest() {
	db, err := Open(testIndexFile, testGalaxyFile)
	t.Require().NoError(err)

	t.db = db
}

func (t *BoltDBTestSuite) TearDownTest() {
	err := t.db.Close()
	t.NoError(err)

	t.NoError(os.Remove(testIndexFile))
	t.NoError(os.Remove(testGalaxyFile))
}

func TestNeutronInRange(t *testing.T) {
	assert.True(t, NeutronInRange([]dump.Body{{Type: "Neutron Star", DistanceToArrival: 0}}))
	assert.False(t, NeutronInRange([]dump.Body{{Type: "???", DistanceToArrival: 0}}))
	assert.False(t, NeutronInRange([]dump.Body{{Type: "Neutron Star", DistanceToArrival: 1000.1}}))
}

func TestScoopableInRange(t *testing.T) {
	assert.True(t, ScoopableInRange([]dump.Body{{Type: "K (Yellow-Orange giant) Star", DistanceToArrival: 0}}))
	assert.False(t, ScoopableInRange([]dump.Body{{Type: "K (Yellow-Orange giant) Star", DistanceToArrival: 1000.1}}))
	assert.False(t, ScoopableInRange([]dump.Body{{Type: "Neutron Star", DistanceToArrival: 0}}))
}

func (t *BoltDBTestSuite) TestImport() {
	insert := []dump.System{
		{
			ID64:        10477373803,
			Name:        "Sol",
			Coordinates: r3.Vec{X: 0, Y: 0, Z: 0},
			Bodies: []dump.Body{
				{
					ID64:              10477373803,
					Name:              "Sol",
					Type:              "Star",
					SubType:           "G (White-Yellow) Star",
					DistanceToArrival: 0,
				},
				{
					ID64:              36028807496337771,
					Name:              "Mercury",
					Type:              "Planet",
					SubType:           "Metal-rich body",
					DistanceToArrival: 209.972702,
				},
			},
		},
	}
	for i := 1; i < 1000; i++ {
		insert = append(insert, dump.System{
			ID64: uint64(i),
			Name: fmt.Sprintf("extra %d", i),
			Coordinates: r3.Vec{
				X: float64(i),
				Y: float64(i),
				Z: float64(i),
			},
		})
	}
	t.Len(insert, 1000)
	insert = append(insert, dump.System{
		ID64:        10477373801,
		Name:        "Sol2",
		Coordinates: r3.Vec{X: 0, Y: 0, Z: 0},
	})
	t.Len(insert, 1001)

	for _, s := range insert {
		err := t.db.InsertSystem(s)
		t.NoError(err)
	}
	t.db.StopInsert()

	systems, err := t.db.PointsWithin(0, 0, 0, 0, 0, 0)
	t.NoError(err)
	t.Len(systems, 2)
	t.Contains(systems, System{ID64: 10477373803, X: 0, Y: 0, Z: 0, IsNeutron: false, IsScoopable: true})
	t.Contains(systems, System{ID64: 10477373801, X: 0, Y: 0, Z: 0, IsNeutron: false, IsScoopable: false})
}

func TestBoltDBTestSuite(t *testing.T) {
	suite.Run(t, &BoltDBTestSuite{})
}
