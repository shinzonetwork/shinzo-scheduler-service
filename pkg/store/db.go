package store

import (
	"context"

	"github.com/sourcenetwork/defradb/client"
	"github.com/sourcenetwork/defradb/client/options"
)

// dbClient is the minimal subset of node.DB used by store operations.
// Production code passes node.DB directly; tests pass a mock.
type dbClient interface {
	ExecRequest(ctx context.Context, request string, opts ...options.Enumerable[options.ExecRequestOptions]) *client.RequestResult
}
