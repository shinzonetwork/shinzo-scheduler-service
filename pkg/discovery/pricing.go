package discovery

import "github.com/shinzonetwork/shinzo-scheduler-service/pkg/store"

// ClearingPrice computes the weighted midpoint between an indexer's ask and a
// host's bid, biased toward the indexer's ask proportional to reliability.
// When reliability is 1.0 the result is the arithmetic midpoint. As reliability
// decreases, the price shifts toward the host's bid.
func ClearingPrice(indexerAsk, hostBid, reliability float64) float64 {
	if reliability <= 0 {
		return hostBid
	}
	return (indexerAsk*reliability + hostBid) / (1 + reliability)
}

// PriceOverlaps returns true when the indexer's pricing falls within the
// host's budget for the given subscription type.
func PriceOverlaps(pricing *store.Pricing, budget *store.HostBudget, subType string) bool {
	if pricing == nil || budget == nil {
		return true // no constraint
	}
	switch subType {
	case store.SubTypeTip:
		if budget.MaxTipPer1kBlocks > 0 && pricing.TipPer1kBlocks > budget.MaxTipPer1kBlocks {
			return false
		}
	case store.SubTypeSnapshot:
		if budget.MaxSnapshotPerRange > 0 && pricing.SnapshotPerRange > budget.MaxSnapshotPerRange {
			return false
		}
	}
	return true
}

// AskForType returns the indexer's per-unit ask price for the given subscription type.
func AskForType(pricing *store.Pricing, subType string) float64 {
	if pricing == nil {
		return 0
	}
	switch subType {
	case store.SubTypeTip:
		return pricing.TipPer1kBlocks
	case store.SubTypeSnapshot:
		return pricing.SnapshotPerRange
	}
	return 0
}

// BidForType returns the host's per-unit max bid for the given subscription type.
func BidForType(budget *store.HostBudget, subType string) float64 {
	if budget == nil {
		return 0
	}
	switch subType {
	case store.SubTypeTip:
		return budget.MaxTipPer1kBlocks
	case store.SubTypeSnapshot:
		return budget.MaxSnapshotPerRange
	}
	return 0
}
