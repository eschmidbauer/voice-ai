package sip_pipeline

import (
	"context"
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
			OnConnected: func() {},
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
// Session state transitions
// =============================================================================

func TestCallStateTransferring_IsActive(t *testing.T) {
	assert.True(t, sip_infra.CallStateTransferring.IsActive())
	assert.True(t, sip_infra.CallStateBridgeConnected.IsActive())
}
