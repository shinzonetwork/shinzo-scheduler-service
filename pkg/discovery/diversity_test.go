package discovery

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDiversityWeight_JustMatched(t *testing.T) {
	// Matched essentially now -> weight near 0.1
	w := DiversityWeight(time.Now(), time.Hour)
	assert.InDelta(t, 0.1, w, 0.05)
}

func TestDiversityWeight_HalfElapsed(t *testing.T) {
	// Matched half a window ago -> linear interpolation: 0.1 + 0.9*0.5 = 0.55
	halfAgo := time.Now().Add(-30 * time.Minute)
	w := DiversityWeight(halfAgo, time.Hour)
	assert.InDelta(t, 0.55, w, 0.05)
}

func TestDiversityWeight_FullyElapsed(t *testing.T) {
	// Matched longer ago than the window -> 1.0
	longAgo := time.Now().Add(-2 * time.Hour)
	w := DiversityWeight(longAgo, time.Hour)
	assert.Equal(t, 1.0, w)
}

func TestDiversityWeight_ExactlyAtWindow(t *testing.T) {
	// Matched exactly one window ago -> should be 1.0
	exactlyAgo := time.Now().Add(-time.Hour)
	w := DiversityWeight(exactlyAgo, time.Hour)
	assert.Equal(t, 1.0, w)
}

func TestDiversityWeight_ZeroWindow(t *testing.T) {
	// Zero duration window -> always 1.0
	w := DiversityWeight(time.Now(), 0)
	assert.Equal(t, 1.0, w)
}

func TestDiversityWeight_NegativeWindow(t *testing.T) {
	// Negative window -> treated as no constraint
	w := DiversityWeight(time.Now(), -time.Hour)
	assert.Equal(t, 1.0, w)
}

func TestMostRecentMatch_Found(t *testing.T) {
	ts := time.Now()
	m := map[string]time.Time{"idx-1": ts, "idx-2": ts.Add(-time.Hour)}
	got, ok := MostRecentMatch(m, "idx-1")
	assert.True(t, ok)
	assert.Equal(t, ts, got)
}

func TestMostRecentMatch_NotFound(t *testing.T) {
	m := map[string]time.Time{"idx-1": time.Now()}
	_, ok := MostRecentMatch(m, "idx-missing")
	assert.False(t, ok)
}

func TestMostRecentMatch_EmptyMap(t *testing.T) {
	_, ok := MostRecentMatch(map[string]time.Time{}, "idx-1")
	assert.False(t, ok)
}

func TestMostRecentMatch_NilMap(t *testing.T) {
	_, ok := MostRecentMatch(nil, "idx-1")
	assert.False(t, ok)
}
