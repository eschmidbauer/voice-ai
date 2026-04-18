package sip_pipeline

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	sip_infra "github.com/rapidaai/api/assistant-api/sip/infra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTransferTestConfig() *sip_infra.Config {
	return &sip_infra.Config{
		Server:            "127.0.0.1",
		Port:              5060,
		Username:          "testuser",
		Password:          "testpass",
		CallerID:          "917943446750",
		RTPPortRangeStart: 10000,
		RTPPortRangeEnd:   10020,
	}
}

func newTransferTestSession(t *testing.T) *sip_infra.Session {
	t.Helper()
	s, err := sip_infra.NewSession(context.Background(), &sip_infra.SessionConfig{
		Config:    newTransferTestConfig(),
		Direction: sip_infra.CallDirectionInbound,
	})
	require.NoError(t, err)
	return s
}

// =============================================================================
// Pipeline routing — TransferInitiated/Connected/Failed routed correctly
// =============================================================================

func TestDispatcher_RoutesTransferStages(t *testing.T) {
	t.Parallel()

	var failedCount atomic.Int32

	d := NewDispatcher(&DispatcherConfig{
		Logger: newPipelineTestLogger(t),
	})
	d.Start(context.Background())

	// Override dispatch to count routing (we can't easily override handlers,
	// but we can verify the pipeline reaches dispatch by checking logs/state)
	// For this test, verify the stages compile and are routable by the dispatcher.
	s := newTransferTestSession(t)

	// Test that OnPipeline doesn't panic for new stage types
	d.OnPipeline(context.Background(),
		sip_infra.TransferInitiatedPipeline{
			ID:        "test-transfer",
			Session:   s,
			TargetURI: "918031405561",
			Config:    newTransferTestConfig(),
			OnConnected: func(_ *sip_infra.RTPHandler) {},
			OnFailed:    func() { failedCount.Add(1) },
		},
	)

	d.OnPipeline(context.Background(),
		sip_infra.TransferConnectedPipeline{
			ID:              "test-transfer",
			InboundSession:  s,
			OutboundSession: newTransferTestSession(t),
		},
	)

	d.OnPipeline(context.Background(),
		sip_infra.TransferFailedPipeline{
			ID:     "test-transfer",
			Reason: "test_failure",
		},
	)

	// Allow dispatcher goroutines to process
	time.Sleep(100 * time.Millisecond)

	// TransferInitiated's OnFailed should fire (nil server)
	assert.True(t, failedCount.Load() > 0, "OnFailed should be called when server is nil")
}

// =============================================================================
// handleTransferInitiated — OnFailed called when MakeBridgeCall fails
// =============================================================================

func TestHandleTransferInitiated_OnFailedCalled(t *testing.T) {
	t.Parallel()

	// MakeBridgeCall requires a running Server which we can't easily mock.
	// Instead, test that when executeTransfer runs with a nil/stopped server,
	// it calls OnFailed.

	var failedCalled atomic.Bool

	d := NewDispatcher(&DispatcherConfig{
		Logger: newPipelineTestLogger(t),
		// server is nil — MakeBridgeCall will fail
	})

	s := newTransferTestSession(t)

	done := make(chan struct{})
	go func() {
		defer close(done)
		d.executeTransfer(context.Background(), sip_infra.TransferInitiatedPipeline{
			ID:        s.GetCallID(),
			Session:   s,
			TargetURI: "918031405561",
			Config:    newTransferTestConfig(),
			OnFailed: func() {
				failedCalled.Store(true)
			},
		})
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("executeTransfer did not return")
	}

	assert.True(t, failedCalled.Load(), "OnFailed should be called when MakeBridgeCall fails")

	// Verify metadata set to "failed"
	if statusVal, ok := s.GetMetadata(sip_infra.MetadataBridgeTransferStatus); ok {
		assert.Equal(t, "failed", statusVal)
	}
}

// =============================================================================
// handleTransferInitiated — CallerID resolution from assistant deployment
// =============================================================================

func TestHandleTransferInitiated_CallerIDResolution(t *testing.T) {
	t.Parallel()

	d := NewDispatcher(&DispatcherConfig{
		Logger: newPipelineTestLogger(t),
	})

	// Config with empty CallerID and no assistant — should still not panic
	cfg := &sip_infra.Config{
		Server:            "127.0.0.1",
		Port:              5060,
		Username:          "testuser",
		Password:          "testpass",
		RTPPortRangeStart: 10000,
		RTPPortRangeEnd:   10020,
	}

	s := newTransferTestSession(t)

	done := make(chan struct{})
	go func() {
		defer close(done)
		d.executeTransfer(context.Background(), sip_infra.TransferInitiatedPipeline{
			ID:        s.GetCallID(),
			Session:   s,
			TargetURI: "918031405561",
			Config:    cfg,
			OnFailed:  func() {},
		})
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("executeTransfer did not return")
	}
}

// =============================================================================
// TransferConnected / TransferFailed handlers don't panic
// =============================================================================

func TestHandleTransferConnected_NoPanic(t *testing.T) {
	t.Parallel()

	d := NewDispatcher(&DispatcherConfig{
		Logger: newPipelineTestLogger(t),
	})

	s := newTransferTestSession(t)
	outbound := newTransferTestSession(t)

	// Should not panic
	d.handleTransferConnected(context.Background(), sip_infra.TransferConnectedPipeline{
		ID:              "test-connected",
		InboundSession:  s,
		OutboundSession: outbound,
	})
}

func TestHandleTransferFailed_NoPanic(t *testing.T) {
	t.Parallel()

	d := NewDispatcher(&DispatcherConfig{
		Logger: newPipelineTestLogger(t),
	})

	d.handleTransferFailed(context.Background(), sip_infra.TransferFailedPipeline{
		ID:     "test-failed",
		Reason: "busy",
	})
}

// =============================================================================
// Pipeline stage types — verify CallID()
// =============================================================================

func TestTransferPipelineStages_CallID(t *testing.T) {
	assert.Equal(t, "call-1", sip_infra.TransferInitiatedPipeline{ID: "call-1"}.CallID())
	assert.Equal(t, "call-2", sip_infra.TransferConnectedPipeline{ID: "call-2"}.CallID())
	assert.Equal(t, "call-3", sip_infra.TransferFailedPipeline{ID: "call-3"}.CallID())
}

// =============================================================================
// handleTransferInitiated — OnTeardown vs OnFailed contract
// =============================================================================

func TestHandleTransferInitiated_OnTeardownNotCalledOnFailure(t *testing.T) {
	t.Parallel()

	// When the server is nil, MakeBridgeCall cannot succeed.
	// OnFailed must be called, and OnTeardown must NOT be called.
	// OnTeardown is reserved for the bridge teardown path (after BridgeTransfer returns).

	var failedCalled atomic.Bool
	var teardownCalled atomic.Bool

	d := NewDispatcher(&DispatcherConfig{
		Logger: newPipelineTestLogger(t),
		// server is nil — MakeBridgeCall will fail
	})

	s := newTransferTestSession(t)

	done := make(chan struct{})
	go func() {
		defer close(done)
		d.executeTransfer(context.Background(), sip_infra.TransferInitiatedPipeline{
			ID:        s.GetCallID(),
			Session:   s,
			TargetURI: "918031405561",
			Config:    newTransferTestConfig(),
			OnFailed: func() {
				failedCalled.Store(true)
			},
			OnTeardown: func() {
				teardownCalled.Store(true)
			},
		})
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("executeTransfer did not return")
	}

	assert.True(t, failedCalled.Load(), "OnFailed must be called when server is nil")
	assert.False(t, teardownCalled.Load(), "OnTeardown must NOT be called on failure — only on bridge teardown")
}

func TestTransferInitiatedPipeline_HasOnTeardownField(t *testing.T) {
	// Compile-time contract: TransferInitiatedPipeline must have an OnTeardown field.
	// If the field is removed or renamed, this test fails at compile time.
	var called bool
	p := sip_infra.TransferInitiatedPipeline{
		ID:        "contract-test",
		OnFailed:  func() {},
		OnTeardown: func() { called = true },
	}
	// Verify the field is callable
	p.OnTeardown()
	assert.True(t, called, "OnTeardown must be callable")
}

// =============================================================================
// Session state transitions
// =============================================================================

func TestCallStateTransferring_IsActive(t *testing.T) {
	assert.True(t, sip_infra.CallStateTransferring.IsActive())
	assert.True(t, sip_infra.CallStateBridgeConnected.IsActive())
}

// =============================================================================
// Transfer metadata — failure path does NOT set outbound call ID or duration
// =============================================================================

func TestHandleTransferInitiated_FailureMetadata(t *testing.T) {
	t.Parallel()

	d := NewDispatcher(&DispatcherConfig{
		Logger: newPipelineTestLogger(t),
		// server is nil — will fail immediately
	})

	s := newTransferTestSession(t)

	done := make(chan struct{})
	go func() {
		defer close(done)
		d.executeTransfer(context.Background(), sip_infra.TransferInitiatedPipeline{
			ID:        s.GetCallID(),
			Session:   s,
			TargetURI: "918031405561",
			Config:    newTransferTestConfig(),
			OnFailed:  func() {},
		})
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("executeTransfer did not return")
	}

	// Status must be "failed"
	statusVal, ok := s.GetMetadata(sip_infra.MetadataBridgeTransferStatus)
	require.True(t, ok, "MetadataBridgeTransferStatus should be set")
	assert.Equal(t, "failed", statusVal)

	// Outbound call ID must NOT be set (we never reached the target)
	_, ok = s.GetMetadata(sip_infra.MetadataBridgeTransferOutboundCallID)
	assert.False(t, ok, "MetadataBridgeTransferOutboundCallID should NOT be set on early failure")

	// Duration must NOT be set (bridge never started)
	_, ok = s.GetMetadata(sip_infra.MetadataBridgeTransferDuration)
	assert.False(t, ok, "MetadataBridgeTransferDuration should NOT be set on early failure")
}

// =============================================================================
// categorizeTransferError — classification logic
// =============================================================================

func TestCategorizeTransferError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		reason   string
		err      error
		expected string
	}{
		{
			name:     "server nil",
			reason:   "server_nil",
			err:      nil,
			expected: "setup",
		},
		{
			name:     "config error",
			reason:   "config_error",
			err:      errors.New("invalid config"),
			expected: "setup",
		},
		{
			name:     "outbound failed with timeout",
			reason:   "outbound_failed",
			err:      errors.New("context deadline exceeded"),
			expected: "network",
		},
		{
			name:     "outbound failed with timeout keyword",
			reason:   "outbound_failed",
			err:      errors.New("dial timeout after 30s"),
			expected: "network",
		},
		{
			name:     "outbound failed with busy",
			reason:   "outbound_failed",
			err:      errors.New("SIP 486 Busy Here"),
			expected: "rejected",
		},
		{
			name:     "outbound failed with 603 declined",
			reason:   "outbound_failed",
			err:      errors.New("received 603 Decline"),
			expected: "rejected",
		},
		{
			name:     "outbound failed generic",
			reason:   "outbound_failed",
			err:      errors.New("connection refused"),
			expected: "network",
		},
		{
			name:     "outbound failed nil error",
			reason:   "outbound_failed",
			err:      nil,
			expected: "network",
		},
		{
			name:     "bridge failed",
			reason:   "bridge_failed",
			err:      errors.New("RTP relay error"),
			expected: "bridge",
		},
		{
			name:     "unknown reason",
			reason:   "something_else",
			err:      nil,
			expected: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := categorizeTransferError(tt.reason, tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// handleTransferConnected — does not panic, logs with remote URI
// =============================================================================

func TestHandleTransferConnected_RichLogging(t *testing.T) {
	t.Parallel()

	d := NewDispatcher(&DispatcherConfig{
		Logger: newPipelineTestLogger(t),
	})

	s := newTransferTestSession(t)
	outbound := newTransferTestSession(t)

	// Should not panic even with minimal session info
	d.handleTransferConnected(context.Background(), sip_infra.TransferConnectedPipeline{
		ID:              "test-connected",
		InboundSession:  s,
		OutboundSession: outbound,
	})
}

// =============================================================================
// handleTransferFailed — categorization in logs
// =============================================================================

func TestHandleTransferFailed_WithCategory(t *testing.T) {
	t.Parallel()

	d := NewDispatcher(&DispatcherConfig{
		Logger: newPipelineTestLogger(t),
	})

	// Should not panic with various error types
	d.handleTransferFailed(context.Background(), sip_infra.TransferFailedPipeline{
		ID:     "test-fail-timeout",
		Error:  errors.New("context deadline exceeded"),
		Reason: "outbound_failed",
	})

	d.handleTransferFailed(context.Background(), sip_infra.TransferFailedPipeline{
		ID:     "test-fail-busy",
		Error:  errors.New("486 Busy Here"),
		Reason: "outbound_failed",
	})

	d.handleTransferFailed(context.Background(), sip_infra.TransferFailedPipeline{
		ID:     "test-fail-nil-err",
		Error:  nil,
		Reason: "server_nil",
	})
}

// =============================================================================
// Metadata constants — verify keys are distinct
// =============================================================================

func TestMetadataKeyConstants_Distinct(t *testing.T) {
	keys := []string{
		sip_infra.MetadataBridgeTransferTarget,
		sip_infra.MetadataBridgeTransferStatus,
		sip_infra.MetadataBridgeTransferDuration,
		sip_infra.MetadataBridgeTransferOutboundCallID,
	}
	seen := make(map[string]bool, len(keys))
	for _, k := range keys {
		assert.False(t, seen[k], "duplicate metadata key: %s", k)
		seen[k] = true
	}
}
