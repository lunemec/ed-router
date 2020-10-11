package ship

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestJumpFuelRemaining(t *testing.T) {
	s := New(32, 346.9, 1692.6, 5, 10.5, 878, FSDRating["A"], FSDClass[5]).(ship)

	jumpDistance := 67.73366678246619
	fuel := s.fuelToJump(jumpDistance)
	s2, err := s.Jump(jumpDistance)
	assert.NoError(t, err)

	expectFuelConsumed := 4.999999999999998
	assert.EqualValues(t, expectFuelConsumed, fuel)
	assert.EqualValues(t, s.fuelTank-expectFuelConsumed, s2.(ship).fuelRemaining)
}

func TestJumpRange(t *testing.T) {
	s := New(32, 346.9, 1692.6, 5, 10.5, 878, FSDRating["A"], FSDClass[5])
	r := s.JumpRange()

	// Strange thing is, that coriolis shows 67.70, but game 67.60
	// I don't think rounding would change that.
	assert.EqualValues(t, 67.73366678246619, r)
	assert.EqualValues(t, 67.73, math.Round(r*100.0)/100.0)
}

func TestJumpRangeIncreases(t *testing.T) {
	s := New(32, 346.9, 1692.6, 5, 10.5, 878, FSDRating["A"], FSDClass[5])

	jumpDistance := 67.73366678246619
	s2, err := s.Jump(jumpDistance)
	assert.NoError(t, err)

	expectFuelConsumed := 4.999999999999998
	assert.EqualValues(t, s.(ship).fuelTank-expectFuelConsumed, s2.(ship).fuelRemaining)

	newRange := s2.JumpRange()
	assert.EqualValues(t, 68.57066103199041, newRange)
}

func TestJumpOutOfFuel(t *testing.T) {
	var (
		err error
		s   Ship
	)
	s = New(32, 346.9, 1692.6, 5, 10.5, 878, FSDRating["A"], FSDClass[5])

	jumpDistance := 67.73366678246619
	s, err = s.Jump(jumpDistance)
	assert.NoError(t, err)
	jumps := 1

	for {
		s, err = s.Jump(s.JumpRange())
		if err != nil {
			break
		}
		jumps++
	}
	sh := s.(ship)
	assert.Equal(t, ErrNotEnoughFuel, err)
	assert.Equal(t, 6, jumps)
	assert.Equal(t, 2.0000000000000053, sh.fuelRemaining)
	assert.EqualValues(t, 30, sh.mass-sh.currentMass)
	assert.Equal(t, 316.9, sh.currentMass)
	assert.Equal(t, 73.15181131851537, s.JumpRange())
}
