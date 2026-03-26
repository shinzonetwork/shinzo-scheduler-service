package discovery

// ApplyTipCurrencyPenalty halves the reliability score of an indexer when its
// current tip lags behind the reference tip by more than lagThreshold blocks.
// This is applied at query time for ranking purposes only; it does not update
// the stored score.
func ApplyTipCurrencyPenalty(score float64, currentTip, referenceTip, lagThreshold int) float64 {
	if referenceTip > 0 && referenceTip-currentTip > lagThreshold {
		return score * 0.5
	}
	return score
}

// UpdateEMA computes the next EMA reliability score given the previous score and
// a boolean indicating whether the latest probe succeeded.
//
// weight = 0.1 gives roughly a 10-sample window.
func UpdateEMA(prevScore float64, success bool) float64 {
	const weight = 0.1
	increment := 0.0
	if success {
		increment = 1.0
	}
	return (1-weight)*prevScore + weight*increment
}
