package boltdb

import (
	"math"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	bolt "go.etcd.io/bbolt"
)

func TestIndexKeyMarshal(t *testing.T) {
	// Handle negative.
	expected := -123.33
	assert.Equal(t, expected, UnmarshalIndexKey(MarshalIndexKey(expected)))

	// Handle 0.
	expected = 0
	assert.Equal(t, expected, UnmarshalIndexKey(MarshalIndexKey(expected)))

	// Handle positive.
	expected = math.MaxInt32
	assert.Equal(t, expected, UnmarshalIndexKey(MarshalIndexKey(expected)))

	// Truncate to 3 decimal places.
	expected = -123.33333
	assert.Equal(t, -123.333, UnmarshalIndexKey(MarshalIndexKey(expected)))
}

func TestIndexKeyBytes(t *testing.T) {
	expected := float64(-1)
	res := MarshalIndexKey(expected)
	assert.Equal(t, []byte{0x0, 0x0, 0x0, 0x0, 0x7f, 0xff, 0xfc, 0x17}, res)

	expected = float64(0)
	res = MarshalIndexKey(expected)
	assert.Equal(t, []byte{0x0, 0x0, 0x0, 0x0, 0x7f, 0xff, 0xff, 0xff}, res)

	expected = float64(1)
	res = MarshalIndexKey(expected)
	assert.Equal(t, []byte{0x0, 0x0, 0x0, 0x0, 0x80, 0x0, 0x3, 0xe7}, res)
}

func TestIndexValueMarshal(t *testing.T) {
	systems := []System{
		{
			ID64:        1,
			X:           2,
			Y:           3,
			Z:           4,
			IsNeutron:   true,
			IsScoopable: true,
		},
	}
	res := MarshalIndexValue(systems)
	expect := []byte{
		0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1, // length of the array uint64
		0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1, // 1st item ID64 uint64
		0x40, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, // 1st item X float64
		0x40, 0x8, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, // 1st item Y float64
		0x40, 0x10, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, // 1st item Z float64
		0x1, // 1st item IsNeutron bool
		0x1, // 1st item IsScoopable bool
	}

	assert.Equal(t, expect, res)
}

func TestIndexValueUnmarshal(t *testing.T) {
	data := []byte{
		0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1, // length of the array uint64
		0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1, // 1st item ID64 uint64
		0x40, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, // 1st item X float64
		0x40, 0x8, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, // 1st item Y float64
		0x40, 0x10, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, // 1st item Z float64
		0x1, // 1st item IsNeutron bool
		0x1, // 1st item IsScoopable bool
	}
	expect := []System{
		{
			ID64:        1,
			X:           2,
			Y:           3,
			Z:           4,
			IsNeutron:   true,
			IsScoopable: true,
		},
	}
	got := UnmarshalIndexValue(data)
	assert.Equal(t, expect, got)
}

func TestIndexValueUnmarshalSol(t *testing.T) {
	data := []byte{
		0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1, // length of the array uint64
		0x0, 0x0, 0x0, 0x2, 0x70, 0x80, 0x9, 0x6b, // 1st item ID64 uint64
		0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, // 1st item X float64
		0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, // 1st item Y float64
		0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, // 1st item Z float64
		0x0, // 1st item IsNeutron bool
		0x1, // 1st item IsScoopable bool
	}

	expect := []System{
		{
			ID64:        10477373803,
			X:           0,
			Y:           0,
			Z:           0,
			IsNeutron:   false,
			IsScoopable: true,
		},
	}
	marshaled := MarshalIndexValue(expect)
	assert.Equal(t, data, marshaled)
	got := UnmarshalIndexValue(data)
	assert.Equal(t, expect, got)
}

func TestIndexValueMarshalUnmarshalMultiple(t *testing.T) {
	expect := []System{
		{
			ID64:        1,
			X:           2,
			Y:           3,
			Z:           4,
			IsNeutron:   true,
			IsScoopable: true,
		},
		{
			ID64:        999,
			X:           2234.234,
			Y:           3.123,
			Z:           4.555,
			IsNeutron:   false,
			IsScoopable: true,
		},
		{
			ID64:        44,
			X:           0,
			Y:           0,
			Z:           0,
			IsNeutron:   false,
			IsScoopable: false,
		},
	}
	data := MarshalIndexValue(expect)
	expectData := []byte{
		0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x3,
		0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1,
		0x40, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0,
		0x40, 0x8, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0,
		0x40, 0x10, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0,
		0x1,
		0x1,
		0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x3, 0xe7,
		0x40, 0xa1, 0x74, 0x77, 0xce, 0xd9, 0x16, 0x87,
		0x40, 0x8, 0xfb, 0xe7, 0x6c, 0x8b, 0x43, 0x96,
		0x40, 0x12, 0x38, 0x51, 0xeb, 0x85, 0x1e, 0xb8,
		0x0,
		0x1,
		0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x2c,
		0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0,
		0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0,
		0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0,
		0x0,
		0x0,
	}

	assert.Equal(t, expectData, data)
	got := UnmarshalIndexValue(data)
	assert.Equal(t, expect, got)
}

var BenchmarkUnmarshalIndexValueSystems []System

func BenchmarkUnmarshalIndexValue(b *testing.B) {
	BenchmarkUnmarshalIndexValueSystems = nil
	expectData := []byte{
		0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x3,
		0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1,
		0x40, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0,
		0x40, 0x8, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0,
		0x40, 0x10, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0,
		0x1,
		0x1,
		0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x3, 0xe7,
		0x40, 0xa1, 0x74, 0x77, 0xce, 0xd9, 0x16, 0x87,
		0x40, 0x8, 0xfb, 0xe7, 0x6c, 0x8b, 0x43, 0x96,
		0x40, 0x12, 0x38, 0x51, 0xeb, 0x85, 0x1e, 0xb8,
		0x0,
		0x1,
		0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x2c,
		0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0,
		0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0,
		0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0,
		0x0,
		0x0,
	}

	expect := []System{
		{
			ID64:        1,
			X:           2,
			Y:           3,
			Z:           4,
			IsNeutron:   true,
			IsScoopable: true,
		},
		{
			ID64:        999,
			X:           2234.234,
			Y:           3.123,
			Z:           4.555,
			IsNeutron:   false,
			IsScoopable: true,
		},
		{
			ID64:        44,
			X:           0,
			Y:           0,
			Z:           0,
			IsNeutron:   false,
			IsScoopable: false,
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// 81.5 ns/op 128 B/op 1 allocs/op
		BenchmarkUnmarshalIndexValueSystems = UnmarshalIndexValue(expectData)
	}

	assert.Equal(b, expect, BenchmarkUnmarshalIndexValueSystems)
}

var IndexKeyUnmarshalBench float64

func BenchmarkIndexKeyUnmarshal(b *testing.B) {
	IndexKeyUnmarshalBench = 0

	bytes := MarshalIndexKey(math.MaxFloat32)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// 1.6 ns/op 0 B/op 0 allocs/op
		IndexKeyUnmarshalBench = UnmarshalIndexKey(bytes)
	}
}

var IndexKeyMarsalBench []byte

func BenchmarkIndexKeyMarshal(b *testing.B) {
	IndexKeyMarsalBench = nil

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// 1.6 ns/op 0 B/op 0 allocs/op
		IndexKeyMarsalBench = MarshalIndexKey(math.MaxFloat32)
	}
}

func (t *BoltDBTestSuite) TestIndexBatchWriter() {
	var systems []interface{}
	for i := 0; i <= 1101; i++ {
		systems = append(systems, System{
			ID64: uint64(i),
			X:    float64(i),
			Y:    float64(i),
			Z:    float64(i),
		})
	}
	err := IndexBatchWriter(t.db.index, systems)
	t.NoError(err)

	var items int
	err = t.db.index.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("x"))
		items = b.Stats().KeyN
		return nil
	})

	t.NoError(err)
	t.Equal(items, len(systems))
}

func (t *BoltDBTestSuite) TestIndexBatchWriterMultipleSameCoordinates() {
	systems := []interface{}{
		System{ID64: 123,
			X: 1, Y: 2, Z: 3},
		System{ID64: 1,
			X: 0, Y: 0, Z: 0},
		System{ID64: 1234,
			X: 1, Y: 2, Z: 3},
	}

	err := IndexBatchWriter(t.db.index, systems)
	t.NoError(err)

	err = t.db.index.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("x"))
		items := b.Stats().KeyN
		t.Equal(2, items)

		t.Equal([]System{systems[1].(System)}, UnmarshalIndexValue(b.Get(MarshalIndexKey(0))))
		t.Equal([]System{systems[0].(System), systems[2].(System)}, UnmarshalIndexValue(b.Get(MarshalIndexKey(1))))
		return nil
	})

	t.NoError(err)
}

func (t *BoltDBTestSuite) TestIndexBatchWriterThousandSameCoordinates() {
	count := 1000
	var systems []interface{}
	for i := 0; i < count; i++ {
		systems = append(systems,
			System{ID64: uint64(i),
				X: 0, Y: 1, Z: 2})
	}

	err := IndexBatchWriter(t.db.index, systems)
	t.NoError(err)

	err = t.db.index.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("x"))
		items := b.Stats().KeyN
		t.Equal(1, items)

		data := UnmarshalIndexValue(b.Get(MarshalIndexKey(0)))
		t.Len(data, count)
		return nil
	})

	t.NoError(err)

	pointsWithin, err := t.db.PointsWithin(0, 0, 1, 1, 2, 2)
	t.NoError(err)
	t.Len(pointsWithin, count)
}

func TestSystemWithinBounds(t *testing.T) {
	is := systemWithinBounds(System{X: 0, Y: 0, Z: 0}, -10, 10, -10, 10, -10, 10)
	assert.True(t, is)

	isNot := systemWithinBounds(System{X: 10.1, Y: -10, Z: 10}, -10, 10, -10, 10, -10, 10)
	assert.False(t, isNot)
}

func (t *BoltDBTestSuite) TestBoltDBPointsWithin() {
	systems := []interface{}{
		System{ID64: 1,
			X: 0, Y: 0, Z: 0},
		System{ID64: 2,
			X: 1, Y: 2, Z: 3},
		System{ID64: 3,
			X: -9, Y: 9, Z: 9},
		System{ID64: 4,
			X: 10.1, Y: 9, Z: 9},
		System{ID64: 5,
			X: 0, Y: -10.01, Z: 9},
		System{ID64: 6,
			X: 0, Y: 9, Z: 10.001},
	}

	err := IndexBatchWriter(t.db.index, systems)
	t.NoError(err)

	err = t.db.index.View(func(tx *bolt.Tx) error {
		xB := tx.Bucket([]byte("x"))
		ids := UnmarshalIndexValue(xB.Get(MarshalIndexKey(0)))
		t.Equal([]System{systems[0].(System), systems[4].(System), systems[5].(System)}, ids)
		return nil
	})
	t.NoError(err)

	points, err := t.db.PointsWithin(-10, 10, -10, 10, -10, 10)
	t.NoError(err)
	t.Contains(points, systems[0])
	t.Contains(points, systems[1])
	t.Contains(points, systems[2])
}

func BenchmarkBoltDBPointsWithin(b *testing.B) {
	db, err := Open(testIndexFile, testGalaxyFile)
	assert.NoError(b, err)

	defer func() {
		db.galaxy.Close()
		db.index.Close()

		assert.NoError(b, os.Remove(testIndexFile))
		assert.NoError(b, os.Remove(testGalaxyFile))
	}()

	systems := []interface{}{
		System{ID64: 1,
			X: 0, Y: 0, Z: 0},
		System{ID64: 2,
			X: 1, Y: 2, Z: 3},
		System{ID64: 3,
			X: -9, Y: 9, Z: 9},
		System{ID64: 4,
			X: 10.1, Y: 9, Z: 9},
		System{ID64: 5,
			X: 0, Y: -10.01, Z: 9},
		System{ID64: 6,
			X: 0, Y: 9, Z: 10.001},
	}

	err = IndexBatchWriter(db.index, systems)
	assert.NoError(b, err)

	err = db.index.View(func(tx *bolt.Tx) error {
		xB := tx.Bucket([]byte("x"))
		ids := UnmarshalIndexValue(xB.Get(MarshalIndexKey(0)))
		assert.Equal(b, []System{systems[0].(System), systems[4].(System), systems[5].(System)}, ids)
		return nil
	})
	assert.NoError(b, err)

	var points []System

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// 4044 ns/op	    2920 B/op	      36 allocs/op
		points, err = db.PointsWithin(-10, 10, -10, 10, -10, 10)
	}
	assert.NoError(b, err)
	assert.Contains(b, points, systems[0])
	assert.Contains(b, points, systems[1])
	assert.Contains(b, points, systems[2])
}

func (t *BoltDBTestSuite) TestIndexInsert() {
	data := []interface{}{
		System{ID64: 1, X: 0, Y: 0, Z: 0},
	}
	err := IndexBatchWriter(t.db.index, data)
	t.NoError(err)

	points, err := t.db.PointsWithin(0, 0, 0, 0, 0, 0)
	t.NoError(err)

	t.Contains(points, data[0])
}
