package schema

import _ "embed"

//go:embed schema.graphql
var GraphQL string

// Collections lists all DefraDB collection names defined by the scheduler schema.
// Used when registering collections of interest for P2P replication.
var Collections = []string{
	"Scheduler__Indexer",
	"Scheduler__Host",
	"Scheduler__Subscription",
	"Scheduler__ProbeResult",
	"Scheduler__Rating",
	"Scheduler__MatchHistory",
	"Scheduler__Contradiction",
	"Scheduler__DeliveryClaim",
	"Scheduler__Attestation",
	"Scheduler__SessionLedger",
	"Scheduler__ComparisonResult",
	"Scheduler__EscrowAccount",
	"Scheduler__Settlement",
	"Scheduler__Verdict",
}
