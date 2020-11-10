package pather

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"gonum.org/v1/gonum/spatial/r3"
)

func TestIsInSphere(t *testing.T) {
	assert.True(t, isInSphere(r3.Vec{0, 0, 0}, r3.Vec{0, 0, 0}, 1))
	assert.False(t, isInSphere(r3.Vec{1, 1, 1}, r3.Vec{0, 0, 0}, 1))
}

var Out bool

func BenchmarkIsInSphere(b *testing.B) {
	// 22920224	        52.2 ns/op	       0 B/op	       0 allocs/op
	for i := 0; i < b.N; i++ {
		Out = isInSphere(r3.Vec{0, 0, 0}, r3.Vec{0, 0, 0}, 1)
		Out = isInSphere(r3.Vec{1, 1, 1}, r3.Vec{0, 0, 0}, 1)
	}
}
