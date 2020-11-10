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

var Out bool

func BenchmarkDistance(b *testing.B) {
	assert.True(b, Distance(r3.Vec{0, 0, 0}, r3.Vec{0, 0, 0}) < 1)
	assert.False(b, Distance(r3.Vec{1, 1, 1}, r3.Vec{0, 0, 0}) < 1)

	// 9759423	       124 ns/op	       0 B/op	       0 allocs/op
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Out = Distance(r3.Vec{0, 0, 0}, r3.Vec{0, 0, 0}) < 1
		Out = Distance(r3.Vec{1, 1, 1}, r3.Vec{0, 0, 0}) < 1
	}
}
