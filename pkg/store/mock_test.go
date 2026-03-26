package store

import (
	"context"
	"errors"

	"github.com/sourcenetwork/defradb/client"
	"github.com/sourcenetwork/defradb/client/options"
)

type mockDB struct {
	result *client.RequestResult
}

func (m *mockDB) ExecRequest(_ context.Context, _ string, _ ...options.Enumerable[options.ExecRequestOptions]) *client.RequestResult {
	return m.result
}

func okResult(data any) *client.RequestResult {
	return &client.RequestResult{GQL: client.GQLResult{Data: data}}
}

func errResult(msg string) *client.RequestResult {
	return &client.RequestResult{GQL: client.GQLResult{Errors: []error{errors.New(msg)}}}
}

// unmarshalableResult returns a result whose Data field cannot be marshaled to JSON.
func unmarshalableResult() *client.RequestResult {
	return &client.RequestResult{GQL: client.GQLResult{Data: make(chan int)}}
}
