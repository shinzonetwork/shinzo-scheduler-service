package discovery

import "time"

func DiversityWeight(lastMatchedAt time.Time, recencyWindow time.Duration) float64 {
	if recencyWindow <= 0 {
		return 1.0
	}
	elapsed := time.Since(lastMatchedAt)
	if elapsed >= recencyWindow {
		return 1.0
	}
	// Linear interpolation from 0.1 (just matched) to 1.0 (recencyWindow ago).
	return 0.1 + 0.9*(float64(elapsed)/float64(recencyWindow))
}

// MostRecentMatch scans a set of match timestamps keyed by indexer ID and
// returns the most recent timestamp for the given indexer.
func MostRecentMatch(matchTimes map[string]time.Time, indexerID string) (time.Time, bool) {
	t, ok := matchTimes[indexerID]
	return t, ok
}
