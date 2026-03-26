package discovery

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUpdateEMA(t *testing.T) {
	const eps = 1e-9

	t.Run("success increments score", func(t *testing.T) {
		got := UpdateEMA(0.5, true)
		assert.InDelta(t, 0.9*0.5+0.1, got, eps)
	})

	t.Run("failure decrements score", func(t *testing.T) {
		got := UpdateEMA(0.5, false)
		assert.InDelta(t, 0.9*0.5, got, eps)
	})

	t.Run("converges to 1 after many successes", func(t *testing.T) {
		score := 0.0
		for i := 0; i < 200; i++ {
			score = UpdateEMA(score, true)
		}
		assert.InDelta(t, 1.0, score, 1e-6)
	})

	t.Run("converges to 0 after many failures", func(t *testing.T) {
		score := 1.0
		for i := 0; i < 200; i++ {
			score = UpdateEMA(score, false)
		}
		assert.InDelta(t, 0.0, score, 1e-6)
	})

	t.Run("starting from zero with success", func(t *testing.T) {
		got := UpdateEMA(0.0, true)
		assert.InDelta(t, 0.1, got, eps)
	})
}

func TestApplyTipCurrencyPenalty(t *testing.T) {
	const threshold = 10

	t.Run("within threshold — no penalty", func(t *testing.T) {
		score := ApplyTipCurrencyPenalty(0.8, 90, 95, threshold)
		assert.InDelta(t, 0.8, score, 1e-9)
	})

	t.Run("exactly at threshold — no penalty", func(t *testing.T) {
		score := ApplyTipCurrencyPenalty(0.8, 90, 100, threshold)
		assert.InDelta(t, 0.8, score, 1e-9)
	})

	t.Run("one block over threshold — halved", func(t *testing.T) {
		score := ApplyTipCurrencyPenalty(0.8, 89, 100, threshold)
		assert.InDelta(t, 0.4, score, 1e-9)
	})

	t.Run("large lag — halved", func(t *testing.T) {
		score := ApplyTipCurrencyPenalty(1.0, 0, 1000, threshold)
		assert.InDelta(t, 0.5, score, 1e-9)
	})

	t.Run("zero reference tip — no penalty", func(t *testing.T) {
		score := ApplyTipCurrencyPenalty(0.8, 0, 0, threshold)
		assert.InDelta(t, 0.8, score, 1e-9)
	})

	t.Run("result stays in [0,1] range after penalty", func(t *testing.T) {
		score := ApplyTipCurrencyPenalty(1.0, 0, 100, threshold)
		assert.True(t, score >= 0 && score <= 1, "score out of range: %f", score)
		_ = math.IsNaN(score) // compile-time import use
	})
}
