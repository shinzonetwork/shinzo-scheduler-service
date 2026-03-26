package discovery

import (
	"math"
	"testing"

	"github.com/shinzonetwork/shinzo-scheduler-service/pkg/store"
	"github.com/stretchr/testify/assert"
)

func TestClearingPrice_FullReliability(t *testing.T) {
	// reliability=1 -> arithmetic midpoint
	got := ClearingPrice(10, 6, 1.0)
	assert.InDelta(t, 8.0, got, 0.001)
}

func TestClearingPrice_ZeroReliability(t *testing.T) {
	got := ClearingPrice(10, 6, 0)
	assert.Equal(t, 6.0, got)
}

func TestClearingPrice_NegativeReliability(t *testing.T) {
	got := ClearingPrice(10, 6, -0.5)
	assert.Equal(t, 6.0, got)
}

func TestClearingPrice_HalfReliability(t *testing.T) {
	// (10*0.5 + 6) / (1 + 0.5) = 11/1.5 = 7.333...
	got := ClearingPrice(10, 6, 0.5)
	assert.InDelta(t, 7.333, got, 0.01)
}

func TestClearingPrice_HighReliability(t *testing.T) {
	// reliability=0.9 -> (10*0.9 + 6) / 1.9 = 15/1.9 = 7.894...
	got := ClearingPrice(10, 6, 0.9)
	assert.InDelta(t, 7.8947, got, 0.01)
}

func TestClearingPrice_EqualAskBid(t *testing.T) {
	got := ClearingPrice(5, 5, 0.7)
	assert.InDelta(t, 5.0, got, 0.001)
}

func TestPriceOverlaps_NilPricing(t *testing.T) {
	assert.True(t, PriceOverlaps(nil, &store.HostBudget{MaxTipPer1kBlocks: 10}, store.SubTypeTip))
}

func TestPriceOverlaps_NilBudget(t *testing.T) {
	assert.True(t, PriceOverlaps(&store.Pricing{TipPer1kBlocks: 5}, nil, store.SubTypeTip))
}

func TestPriceOverlaps_BothNil(t *testing.T) {
	assert.True(t, PriceOverlaps(nil, nil, store.SubTypeTip))
}

func TestPriceOverlaps_TipWithinBudget(t *testing.T) {
	p := &store.Pricing{TipPer1kBlocks: 5}
	b := &store.HostBudget{MaxTipPer1kBlocks: 10}
	assert.True(t, PriceOverlaps(p, b, store.SubTypeTip))
}

func TestPriceOverlaps_TipExceedsBudget(t *testing.T) {
	p := &store.Pricing{TipPer1kBlocks: 15}
	b := &store.HostBudget{MaxTipPer1kBlocks: 10}
	assert.False(t, PriceOverlaps(p, b, store.SubTypeTip))
}

func TestPriceOverlaps_TipZeroBudget(t *testing.T) {
	// zero budget means no constraint
	p := &store.Pricing{TipPer1kBlocks: 100}
	b := &store.HostBudget{MaxTipPer1kBlocks: 0}
	assert.True(t, PriceOverlaps(p, b, store.SubTypeTip))
}

func TestPriceOverlaps_SnapshotWithinBudget(t *testing.T) {
	p := &store.Pricing{SnapshotPerRange: 3}
	b := &store.HostBudget{MaxSnapshotPerRange: 5}
	assert.True(t, PriceOverlaps(p, b, store.SubTypeSnapshot))
}

func TestPriceOverlaps_SnapshotExceedsBudget(t *testing.T) {
	p := &store.Pricing{SnapshotPerRange: 10}
	b := &store.HostBudget{MaxSnapshotPerRange: 5}
	assert.False(t, PriceOverlaps(p, b, store.SubTypeSnapshot))
}

func TestPriceOverlaps_SnapshotZeroBudget(t *testing.T) {
	p := &store.Pricing{SnapshotPerRange: 100}
	b := &store.HostBudget{MaxSnapshotPerRange: 0}
	assert.True(t, PriceOverlaps(p, b, store.SubTypeSnapshot))
}

func TestPriceOverlaps_UnknownType(t *testing.T) {
	p := &store.Pricing{TipPer1kBlocks: 100, SnapshotPerRange: 100}
	b := &store.HostBudget{MaxTipPer1kBlocks: 1, MaxSnapshotPerRange: 1}
	assert.True(t, PriceOverlaps(p, b, "unknown"))
}

func TestAskForType_Tip(t *testing.T) {
	p := &store.Pricing{TipPer1kBlocks: 7.5, SnapshotPerRange: 3.0}
	assert.Equal(t, 7.5, AskForType(p, store.SubTypeTip))
}

func TestAskForType_Snapshot(t *testing.T) {
	p := &store.Pricing{TipPer1kBlocks: 7.5, SnapshotPerRange: 3.0}
	assert.Equal(t, 3.0, AskForType(p, store.SubTypeSnapshot))
}

func TestAskForType_Nil(t *testing.T) {
	assert.Equal(t, 0.0, AskForType(nil, store.SubTypeTip))
}

func TestAskForType_Unknown(t *testing.T) {
	p := &store.Pricing{TipPer1kBlocks: 7.5}
	assert.Equal(t, 0.0, AskForType(p, "unknown"))
}

func TestBidForType_Tip(t *testing.T) {
	b := &store.HostBudget{MaxTipPer1kBlocks: 12, MaxSnapshotPerRange: 4}
	assert.Equal(t, 12.0, BidForType(b, store.SubTypeTip))
}

func TestBidForType_Snapshot(t *testing.T) {
	b := &store.HostBudget{MaxTipPer1kBlocks: 12, MaxSnapshotPerRange: 4}
	assert.Equal(t, 4.0, BidForType(b, store.SubTypeSnapshot))
}

func TestBidForType_Nil(t *testing.T) {
	assert.Equal(t, 0.0, BidForType(nil, store.SubTypeTip))
}

func TestBidForType_Unknown(t *testing.T) {
	b := &store.HostBudget{MaxTipPer1kBlocks: 12}
	assert.Equal(t, 0.0, BidForType(b, "unknown"))
}

// Verify the formula is monotonically increasing with reliability.
func TestClearingPrice_Monotonic(t *testing.T) {
	prev := math.Inf(-1)
	for r := 0.0; r <= 1.0; r += 0.1 {
		p := ClearingPrice(10, 2, r)
		assert.GreaterOrEqual(t, p, prev)
		prev = p
	}
}
