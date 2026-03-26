package payment

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// buildTestSubscriber creates a subscriber with a no-op logger.
func buildTestSubscriber() *ShinzoHubSubscriber {
	log, _ := zap.NewDevelopment()
	return &ShinzoHubSubscriber{log: log.Sugar()}
}

// makeTxMsg constructs a raw WebSocket envelope matching the dispatch format.
func makeTxMsg(eventType string, attrs map[string]string) []byte {
	return makeTxMsgAtHeight(eventType, attrs, 0)
}

func makeTxMsgAtHeight(eventType string, attrs map[string]string, height int64) []byte {
	type attr struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	var rawAttrs []attr
	for k, v := range attrs {
		rawAttrs = append(rawAttrs, attr{Key: k, Value: v})
	}
	type event struct {
		Type       string `json:"type"`
		Attributes []attr `json:"attributes"`
	}
	envelope := map[string]any{
		"result": map[string]any{
			"data": map[string]any{
				"value": map[string]any{
					"TxResult": map[string]any{
						"height": height,
						"result": map[string]any{
							"events": []event{{Type: eventType, Attributes: rawAttrs}},
						},
					},
				},
			},
		},
	}
	raw, _ := json.Marshal(envelope)
	return raw
}

func TestDispatch_SubscriptionCreated_Valid(t *testing.T) {
	s := buildTestSubscriber()
	payload, _ := json.Marshal(SubscriptionCreatedEvent{
		SubscriptionID: "sub-1",
		HostID:         "host-1",
		IndexerID:      "idx-1",
		ExpiresAt:      "2026-12-31T00:00:00Z",
	})

	var received *SubscriptionCreatedEvent
	s.OnSubscriptionCreated = func(e SubscriptionCreatedEvent) { received = &e }

	s.dispatch(context.Background(), makeTxMsg(EventSubscriptionCreated, map[string]string{"data": string(payload)}))

	assert.NotNil(t, received)
	assert.Equal(t, "sub-1", received.SubscriptionID)
	assert.Equal(t, "host-1", received.HostID)
}

func TestDispatch_SubscriptionCreated_MissingFields(t *testing.T) {
	s := buildTestSubscriber()
	// Payload missing host_id, indexer_id, expires_at.
	payload, _ := json.Marshal(SubscriptionCreatedEvent{SubscriptionID: "sub-2"})

	var called bool
	s.OnSubscriptionCreated = func(_ SubscriptionCreatedEvent) { called = true }

	s.dispatch(context.Background(), makeTxMsg(EventSubscriptionCreated, map[string]string{"data": string(payload)}))

	assert.False(t, called, "callback must not fire when required fields are absent")
}

func TestDispatch_SubscriptionCreated_MissingDataAttr(t *testing.T) {
	s := buildTestSubscriber()
	var called bool
	s.OnSubscriptionCreated = func(_ SubscriptionCreatedEvent) { called = true }

	// No "data" attribute at all.
	s.dispatch(context.Background(), makeTxMsg(EventSubscriptionCreated, map[string]string{}))
	assert.False(t, called)
}

func TestDispatch_SubscriptionExpired_Valid(t *testing.T) {
	s := buildTestSubscriber()
	var received string
	s.OnSubscriptionExpired = func(e SubscriptionExpiredEvent) { received = e.SubscriptionID }

	s.dispatch(context.Background(), makeTxMsg(EventSubscriptionExpired, map[string]string{"subscription_id": "sub-3"}))

	assert.Equal(t, "sub-3", received)
}

func TestDispatch_SubscriptionExpired_MissingID(t *testing.T) {
	s := buildTestSubscriber()
	var called bool
	s.OnSubscriptionExpired = func(_ SubscriptionExpiredEvent) { called = true }

	s.dispatch(context.Background(), makeTxMsg(EventSubscriptionExpired, map[string]string{}))
	assert.False(t, called)
}

func TestDispatch_UnknownEvent(t *testing.T) {
	s := buildTestSubscriber()
	var called bool
	s.OnSubscriptionCreated = func(_ SubscriptionCreatedEvent) { called = true }
	s.OnSubscriptionExpired = func(_ SubscriptionExpiredEvent) { called = true }

	s.dispatch(context.Background(), makeTxMsg("UnknownEvent", map[string]string{"foo": "bar"}))
	assert.False(t, called)
}

func TestDispatch_MalformedJSON(t *testing.T) {
	s := buildTestSubscriber()
	var called bool
	s.OnSubscriptionCreated = func(_ SubscriptionCreatedEvent) { called = true }

	s.dispatch(context.Background(), []byte("not json"))
	assert.False(t, called)
}

func TestWaitEpochThenActivate(t *testing.T) {
	// The status endpoint returns increasing heights: first below target, then at/above.
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := callCount.Add(1)
		var height int64
		if n < 3 {
			height = 100 // below target (fromHeight=100, epochSize=5 → target=105)
		} else {
			height = 106 // at/above target
		}
		fmt.Fprintf(w, `{"result":{"sync_info":{"latest_block_height":"%d"}}}`, height)
	}))
	defer srv.Close()

	// Strip "http://" prefix since GetChainHeight prepends it.
	rpcURL := srv.URL[len("http://"):]
	log, _ := zap.NewDevelopment()
	s := &ShinzoHubSubscriber{rpcURL: rpcURL, epochSize: 5, log: log.Sugar()}

	done := make(chan SubscriptionCreatedEvent, 1)
	s.OnSubscriptionCreated = func(e SubscriptionCreatedEvent) { done <- e }

	ev := SubscriptionCreatedEvent{
		SubscriptionID: "sub-epoch",
		HostID:         "h",
		IndexerID:      "i",
		ExpiresAt:      "2027-01-01T00:00:00Z",
	}

	go s.waitEpochThenActivate(context.Background(), 100, ev)

	received := <-done
	require.Equal(t, "sub-epoch", received.SubscriptionID)
	assert.GreaterOrEqual(t, callCount.Load(), int32(3))
}

func TestDispatch_EpochSize_SpawnsGoroutine(t *testing.T) {
	// With epochSize > 0 and a non-zero event height, the callback should NOT
	// be called synchronously — it is deferred to waitEpochThenActivate.
	log, _ := zap.NewDevelopment()
	s := &ShinzoHubSubscriber{epochSize: 5, log: log.Sugar()}

	var called bool
	s.OnSubscriptionCreated = func(_ SubscriptionCreatedEvent) { called = true }

	payload, _ := json.Marshal(SubscriptionCreatedEvent{
		SubscriptionID: "sub-e",
		HostID:         "h",
		IndexerID:      "i",
		ExpiresAt:      "2027-01-01T00:00:00Z",
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately so waitEpochThenActivate exits without firing

	s.dispatch(ctx, makeTxMsgAtHeight(EventSubscriptionCreated,
		map[string]string{"data": string(payload)}, 50))

	assert.False(t, called, "callback must not fire synchronously when epoch > 0")
}

func TestValidateSubscriptionCreated(t *testing.T) {
	good := SubscriptionCreatedEvent{
		SubscriptionID: "s", HostID: "h", IndexerID: "i", ExpiresAt: "2026-01-01T00:00:00Z",
	}
	assert.NoError(t, validateSubscriptionCreated(good))

	bad := SubscriptionCreatedEvent{SubscriptionID: "s"}
	assert.Error(t, validateSubscriptionCreated(bad))
}

func TestGetChainHeight_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/status", r.URL.Path)
		fmt.Fprint(w, `{"result":{"sync_info":{"latest_block_height":"42"}}}`)
	}))
	defer srv.Close()
	rpcURL := srv.URL[len("http://"):]
	log, _ := zap.NewDevelopment()
	s := &ShinzoHubSubscriber{rpcURL: rpcURL, log: log.Sugar()}
	h, err := s.GetChainHeight(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(42), h)
}

func TestGetChainHeight_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	rpcURL := srv.URL[len("http://"):]
	log, _ := zap.NewDevelopment()
	s := &ShinzoHubSubscriber{rpcURL: rpcURL, log: log.Sugar()}
	_, err := s.GetChainHeight(context.Background())
	assert.Error(t, err)
}

func TestGetChainHeight_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "not-json")
	}))
	defer srv.Close()
	rpcURL := srv.URL[len("http://"):]
	log, _ := zap.NewDevelopment()
	s := &ShinzoHubSubscriber{rpcURL: rpcURL, log: log.Sugar()}
	_, err := s.GetChainHeight(context.Background())
	assert.Error(t, err)
}

func TestGetChainHeight_InvalidHeight(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"result":{"sync_info":{"latest_block_height":"not-a-number"}}}`)
	}))
	defer srv.Close()
	rpcURL := srv.URL[len("http://"):]
	log, _ := zap.NewDevelopment()
	s := &ShinzoHubSubscriber{rpcURL: rpcURL, log: log.Sugar()}
	_, err := s.GetChainHeight(context.Background())
	assert.Error(t, err)
}

func TestWaitEpochThenActivate_ContextCancellation(t *testing.T) {
	// Server always returns a height below the target so the loop never fires the callback.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"result":{"sync_info":{"latest_block_height":"1"}}}`)
	}))
	defer srv.Close()

	rpcURL := srv.URL[len("http://"):]
	log, _ := zap.NewDevelopment()
	s := &ShinzoHubSubscriber{rpcURL: rpcURL, epochSize: 100, log: log.Sugar()}

	var called bool
	s.OnSubscriptionCreated = func(_ SubscriptionCreatedEvent) { called = true }

	ctx, cancel := context.WithCancel(context.Background())
	ev := SubscriptionCreatedEvent{SubscriptionID: "sub-cancel", HostID: "h", IndexerID: "i", ExpiresAt: "2027-01-01T00:00:00Z"}

	done := make(chan struct{})
	go func() {
		s.waitEpochThenActivate(ctx, 0, ev)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("goroutine did not exit after context cancel")
	}
	assert.False(t, called)
}

func TestAttributeMap(t *testing.T) {
	attrs := []struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}{
		{Key: "a", Value: "1"},
		{Key: "b", Value: "2"},
	}
	m := attributeMap(attrs)
	assert.Equal(t, "1", m["a"])
	assert.Equal(t, "2", m["b"])
}

func TestNewShinzoHubSubscriber(t *testing.T) {
	log, _ := zap.NewDevelopment()
	s := NewShinzoHubSubscriber("localhost:26657", 5, log.Sugar())
	assert.NotNil(t, s)
	assert.Equal(t, "localhost:26657", s.rpcURL)
	assert.Equal(t, 5, s.epochSize)
}

func TestGetChainHeight_ConnectionRefused(t *testing.T) {
	log, _ := zap.NewDevelopment()
	s := &ShinzoHubSubscriber{rpcURL: "127.0.0.1:1", log: log.Sugar()}
	_, err := s.GetChainHeight(context.Background())
	assert.Error(t, err)
}

func TestWaitEpochThenActivate_GetHeightError(t *testing.T) {
	log, _ := zap.NewDevelopment()
	// Use unreachable URL so GetChainHeight always errors; ctx cancelled to stop loop.
	s := &ShinzoHubSubscriber{rpcURL: "127.0.0.1:1", epochSize: 5, log: log.Sugar()}

	var called bool
	s.OnSubscriptionCreated = func(_ SubscriptionCreatedEvent) { called = true }

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ev := SubscriptionCreatedEvent{SubscriptionID: "x", HostID: "h", IndexerID: "i", ExpiresAt: "2027-01-01T00:00:00Z"}
	// Should retry on error and eventually exit when ctx is cancelled.
	s.waitEpochThenActivate(ctx, 0, ev)
	assert.False(t, called)
}

func TestDispatch_NilOnSubscriptionCreated(t *testing.T) {
	s := buildTestSubscriber()
	// No OnSubscriptionCreated set — should not panic
	payload, _ := json.Marshal(SubscriptionCreatedEvent{
		SubscriptionID: "sub-1", HostID: "h", IndexerID: "i", ExpiresAt: "2026-01-01T00:00:00Z",
	})
	s.dispatch(context.Background(), makeTxMsg(EventSubscriptionCreated, map[string]string{"data": string(payload)}))
}

func TestDispatch_NilOnSubscriptionExpired(t *testing.T) {
	s := buildTestSubscriber()
	// No OnSubscriptionExpired set — should not panic
	s.dispatch(context.Background(), makeTxMsg(EventSubscriptionExpired, map[string]string{"subscription_id": "sub-1"}))
}

func TestValidateSubscriptionCreated_MissingSubscriptionID(t *testing.T) {
	err := validateSubscriptionCreated(SubscriptionCreatedEvent{
		HostID: "h", IndexerID: "i", ExpiresAt: "2026-01-01T00:00:00Z",
	})
	assert.Error(t, err)
}

// --- Mock WebSocket for Start() tests ---

type mockWSConn struct {
	writeErr    error
	readMsgs    [][]byte // messages returned by ReadMessage in order
	readIdx     int
	readErr     error // returned after readMsgs exhausted
	closeCalled bool
}

func (m *mockWSConn) WriteJSON(_ any) error { return m.writeErr }
func (m *mockWSConn) ReadMessage() (int, []byte, error) {
	if m.readIdx < len(m.readMsgs) {
		msg := m.readMsgs[m.readIdx]
		m.readIdx++
		return 1, msg, nil
	}
	if m.readErr != nil {
		return 0, nil, m.readErr
	}
	// Block until test context cancelled (simulate idle connection).
	time.Sleep(50 * time.Millisecond)
	return 0, nil, fmt.Errorf("connection closed")
}
func (m *mockWSConn) Close() error { m.closeCalled = true; return nil }

type mockDialer struct {
	conn    *mockWSConn
	dialErr error
}

func (m *mockDialer) Dial(_ string, _ http.Header) (wsConn, *http.Response, error) {
	if m.dialErr != nil {
		return nil, nil, m.dialErr
	}
	return m.conn, nil, nil
}

func TestStart_DialFailure(t *testing.T) {
	log, _ := zap.NewDevelopment()
	s := NewShinzoHubSubscriber("ws://fake", 0, log.Sugar())
	s.WithDialer(&mockDialer{dialErr: fmt.Errorf("connection refused")})

	err := s.Start(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "shinzohub connect")
}

func TestStart_SubscribeWriteFailure(t *testing.T) {
	conn := &mockWSConn{writeErr: fmt.Errorf("write broken")}
	log, _ := zap.NewDevelopment()
	s := NewShinzoHubSubscriber("ws://fake", 0, log.Sugar())
	s.WithDialer(&mockDialer{conn: conn})

	err := s.Start(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "shinzohub subscribe")
	assert.True(t, conn.closeCalled)
}

func TestStart_SubscribeAckReadFailure(t *testing.T) {
	// First WriteJSON succeeds, but ReadMessage fails on the ack.
	conn := &mockWSConn{readErr: fmt.Errorf("ack timeout")}
	log, _ := zap.NewDevelopment()
	s := NewShinzoHubSubscriber("ws://fake", 0, log.Sugar())
	s.WithDialer(&mockDialer{conn: conn})

	err := s.Start(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "shinzohub subscribe ack")
	assert.True(t, conn.closeCalled)
}

func TestStart_Success_DispatchesEvent(t *testing.T) {
	payload, _ := json.Marshal(SubscriptionCreatedEvent{
		SubscriptionID: "sub-ws", HostID: "h", IndexerID: "i", ExpiresAt: "2027-01-01T00:00:00Z",
	})
	eventMsg := makeTxMsg(EventSubscriptionCreated, map[string]string{"data": string(payload)})

	// Two ack messages (for 2 subscriptions), then one event, then read error to exit loop.
	conn := &mockWSConn{
		readMsgs: [][]byte{
			[]byte(`{"id":1}`), // ack 1
			[]byte(`{"id":2}`), // ack 2
			eventMsg,           // real event
		},
		readErr: fmt.Errorf("connection closed"),
	}
	log, _ := zap.NewDevelopment()
	s := NewShinzoHubSubscriber("ws://fake", 0, log.Sugar())
	s.WithDialer(&mockDialer{conn: conn})

	received := make(chan SubscriptionCreatedEvent, 1)
	s.OnSubscriptionCreated = func(e SubscriptionCreatedEvent) { received <- e }

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := s.Start(ctx)
	require.NoError(t, err)

	select {
	case ev := <-received:
		assert.Equal(t, "sub-ws", ev.SubscriptionID)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestStart_ReaderExitsOnReadError(t *testing.T) {
	// Two acks, then immediate read error.
	conn := &mockWSConn{
		readMsgs: [][]byte{[]byte(`{"id":1}`), []byte(`{"id":2}`)},
		readErr:  fmt.Errorf("ws broken"),
	}
	log, _ := zap.NewDevelopment()
	s := NewShinzoHubSubscriber("ws://fake", 0, log.Sugar())
	s.WithDialer(&mockDialer{conn: conn})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := s.Start(ctx)
	require.NoError(t, err)
	// Give goroutines time to exit.
	time.Sleep(100 * time.Millisecond)
	assert.True(t, conn.closeCalled)
}

func TestStart_ContextCancellationStopsGoroutines(t *testing.T) {
	// Two acks, then block on reads (simulating idle connection).
	conn := &mockWSConn{
		readMsgs: [][]byte{[]byte(`{"id":1}`), []byte(`{"id":2}`)},
		readErr:  fmt.Errorf("connection closed"),
	}
	log, _ := zap.NewDevelopment()
	s := NewShinzoHubSubscriber("ws://fake", 0, log.Sugar())
	s.WithDialer(&mockDialer{conn: conn})

	ctx, cancel := context.WithCancel(context.Background())
	err := s.Start(ctx)
	require.NoError(t, err)

	// Cancel context — both keep-alive and reader goroutines should exit.
	cancel()
	time.Sleep(100 * time.Millisecond)
}

func TestStart_SecondSubscribeWriteFailure(t *testing.T) {
	// First subscription write+ack succeeds, second write fails.
	writeCount := 0
	conn := &mockWSConn{
		readMsgs: [][]byte{[]byte(`{"id":1}`)}, // only one ack
		readErr:  fmt.Errorf("read fail"),
	}
	// Override WriteJSON to fail on second call.
	origConn := conn
	var writeErrAfterN int = 1
	d := &countingDialer{conn: origConn, writeErrAfterN: writeErrAfterN, writeCount: &writeCount}

	log, _ := zap.NewDevelopment()
	s := NewShinzoHubSubscriber("ws://fake", 0, log.Sugar())
	s.WithDialer(d)

	err := s.Start(context.Background())
	// Should fail on second subscribe write.
	assert.Error(t, err)
}

type countingDialer struct {
	conn           *mockWSConn
	writeErrAfterN int
	writeCount     *int
}

func (d *countingDialer) Dial(_ string, _ http.Header) (wsConn, *http.Response, error) {
	return &countingWSConn{inner: d.conn, errAfterN: d.writeErrAfterN, count: d.writeCount}, nil, nil
}

type countingWSConn struct {
	inner     *mockWSConn
	errAfterN int
	count     *int
}

func (c *countingWSConn) WriteJSON(v any) error {
	*c.count++
	if *c.count > c.errAfterN {
		c.inner.closeCalled = true
		return fmt.Errorf("write failed on call %d", *c.count)
	}
	return nil
}
func (c *countingWSConn) ReadMessage() (int, []byte, error) { return c.inner.ReadMessage() }
func (c *countingWSConn) Close() error                      { return c.inner.Close() }

func TestWithDialer(t *testing.T) {
	log, _ := zap.NewDevelopment()
	s := NewShinzoHubSubscriber("ws://fake", 0, log.Sugar())
	d := &mockDialer{}
	s.WithDialer(d)
	assert.Equal(t, d, s.dialer)
}

func TestDispatch_InvalidCreatedData(t *testing.T) {
	s := buildTestSubscriber()
	var called bool
	s.OnSubscriptionCreated = func(_ SubscriptionCreatedEvent) { called = true }
	// "data" attribute is not valid JSON.
	s.dispatch(context.Background(), makeTxMsg(EventSubscriptionCreated, map[string]string{"data": "not-json"}))
	assert.False(t, called)
}

func TestDispatch_EmptyEnvelope(t *testing.T) {
	s := buildTestSubscriber()
	s.dispatch(context.Background(), []byte(`{}`))
}

func TestGetChainHeight_RequestCreationError(t *testing.T) {
	log, _ := zap.NewDevelopment()
	s := &ShinzoHubSubscriber{rpcURL: "://invalid", log: log.Sugar()}
	_, err := s.GetChainHeight(context.Background())
	assert.Error(t, err)
}
