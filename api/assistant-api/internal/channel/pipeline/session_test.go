// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

package channel_pipeline

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	callcontext "github.com/rapidaai/api/assistant-api/internal/callcontext"
	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
	"github.com/rapidaai/pkg/commons"
	"github.com/rapidaai/protos"
)

func newTestLogger() commons.Logger {
	l, _ := commons.NewApplicationLogger(commons.Level("error"), commons.Name("test"))
	return l
}

func TestSessionConnected_ReaderWriterPassedToStreamer(t *testing.T) {
	var capturedReader *bufio.Reader
	var capturedWriter *bufio.Writer

	d := NewDispatcher(&DispatcherConfig{
		Logger: newTestLogger(),
		OnResolveSession: func(ctx context.Context, contextID string) (*callcontext.CallContext, *protos.VaultCredential, error) {
			return &callcontext.CallContext{Provider: "asterisk"}, &protos.VaultCredential{}, nil
		},
		OnCreateStreamer: func(ctx context.Context, cc *callcontext.CallContext, vc *protos.VaultCredential, ws *websocket.Conn, conn net.Conn, reader *bufio.Reader, writer *bufio.Writer) (internal_type.Streamer, error) {
			capturedReader = reader
			capturedWriter = writer
			// Return an error to short-circuit the rest of the pipeline (avoids needing Talking stub)
			return nil, errors.New("streamer-test-stop")
		},
	})

	ctx := context.Background()
	d.Start(ctx)

	reader := bufio.NewReader(&bytes.Buffer{})
	writer := bufio.NewWriter(&bytes.Buffer{})

	result := d.RunSync(ctx, SessionConnectedPipeline{
		ID:        "test-call",
		ContextID: "test-ctx",
		Reader:    reader,
		Writer:    writer,
	})

	if result.Error == nil || result.Error.Error() != "streamer-test-stop" {
		t.Fatalf("expected streamer-test-stop error, got: %v", result.Error)
	}
	if capturedReader != reader {
		t.Error("reader was not passed to OnCreateStreamer")
	}
	if capturedWriter != writer {
		t.Error("writer was not passed to OnCreateStreamer")
	}
}

func TestSessionConnected_NilReaderWriter(t *testing.T) {
	var capturedReader *bufio.Reader
	var capturedWriter *bufio.Writer
	streamerCalled := false

	d := NewDispatcher(&DispatcherConfig{
		Logger: newTestLogger(),
		OnResolveSession: func(ctx context.Context, contextID string) (*callcontext.CallContext, *protos.VaultCredential, error) {
			return &callcontext.CallContext{Provider: "twilio"}, &protos.VaultCredential{}, nil
		},
		OnCreateStreamer: func(ctx context.Context, cc *callcontext.CallContext, vc *protos.VaultCredential, ws *websocket.Conn, conn net.Conn, reader *bufio.Reader, writer *bufio.Writer) (internal_type.Streamer, error) {
			streamerCalled = true
			capturedReader = reader
			capturedWriter = writer
			return nil, errors.New("streamer-test-stop")
		},
	})

	ctx := context.Background()
	d.Start(ctx)

	result := d.RunSync(ctx, SessionConnectedPipeline{
		ID:        "ws-call",
		ContextID: "ws-ctx",
	})

	if result.Error == nil || result.Error.Error() != "streamer-test-stop" {
		t.Fatalf("expected streamer-test-stop error, got: %v", result.Error)
	}
	if !streamerCalled {
		t.Fatal("OnCreateStreamer was not called")
	}
	if capturedReader != nil {
		t.Error("reader should be nil for WebSocket-only pipeline")
	}
	if capturedWriter != nil {
		t.Error("writer should be nil for WebSocket-only pipeline")
	}
}

func TestSessionConnected_ResolveSessionError(t *testing.T) {
	resolveErr := errors.New("session not found")

	d := NewDispatcher(&DispatcherConfig{
		Logger: newTestLogger(),
		OnResolveSession: func(ctx context.Context, contextID string) (*callcontext.CallContext, *protos.VaultCredential, error) {
			return nil, nil, resolveErr
		},
	})

	ctx := context.Background()
	d.Start(ctx)

	result := d.RunSync(ctx, SessionConnectedPipeline{
		ID:        "fail-call",
		ContextID: "bad-ctx",
	})

	if result.Error == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(result.Error, resolveErr) {
		t.Errorf("expected resolveErr, got: %v", result.Error)
	}
}

func TestSessionConnected_MissingResolveCallback(t *testing.T) {
	d := NewDispatcher(&DispatcherConfig{
		Logger: newTestLogger(),
		// OnResolveSession deliberately nil
	})

	ctx := context.Background()
	d.Start(ctx)

	result := d.RunSync(ctx, SessionConnectedPipeline{
		ID:        "no-resolve",
		ContextID: "no-resolve-ctx",
	})

	if !errors.Is(result.Error, ErrCallbackNotConfigured) {
		t.Errorf("expected ErrCallbackNotConfigured, got: %v", result.Error)
	}
}

func TestSessionConnected_MissingStreamerCallback(t *testing.T) {
	d := NewDispatcher(&DispatcherConfig{
		Logger: newTestLogger(),
		OnResolveSession: func(ctx context.Context, contextID string) (*callcontext.CallContext, *protos.VaultCredential, error) {
			return &callcontext.CallContext{Provider: "asterisk"}, &protos.VaultCredential{}, nil
		},
		// OnCreateStreamer deliberately nil
	})

	ctx := context.Background()
	d.Start(ctx)

	result := d.RunSync(ctx, SessionConnectedPipeline{
		ID:        "no-streamer",
		ContextID: "no-streamer-ctx",
	})

	if !errors.Is(result.Error, ErrCallbackNotConfigured) {
		t.Errorf("expected ErrCallbackNotConfigured, got: %v", result.Error)
	}
}

func TestSessionConnected_ContextCanceled(t *testing.T) {
	d := NewDispatcher(&DispatcherConfig{
		Logger: newTestLogger(),
		OnResolveSession: func(ctx context.Context, contextID string) (*callcontext.CallContext, *protos.VaultCredential, error) {
			<-ctx.Done()
			return nil, nil, ctx.Err()
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	d.Start(ctx)

	result := d.RunSync(ctx, SessionConnectedPipeline{
		ID:        "cancel-call",
		ContextID: "cancel-ctx",
	})

	if result.Error == nil {
		t.Fatal("expected error from canceled context")
	}
}

func TestSessionConnected_ConnFieldPassedThrough(t *testing.T) {
	var capturedConn net.Conn

	d := NewDispatcher(&DispatcherConfig{
		Logger: newTestLogger(),
		OnResolveSession: func(ctx context.Context, contextID string) (*callcontext.CallContext, *protos.VaultCredential, error) {
			return &callcontext.CallContext{Provider: "asterisk"}, &protos.VaultCredential{}, nil
		},
		OnCreateStreamer: func(ctx context.Context, cc *callcontext.CallContext, vc *protos.VaultCredential, ws *websocket.Conn, conn net.Conn, reader *bufio.Reader, writer *bufio.Writer) (internal_type.Streamer, error) {
			capturedConn = conn
			return nil, errors.New("stop")
		},
	})

	ctx := context.Background()
	d.Start(ctx)

	// Use a pipe to create a real net.Conn
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	d.RunSync(ctx, SessionConnectedPipeline{
		ID:        "conn-test",
		ContextID: "conn-ctx",
		Conn:      client,
	})

	if capturedConn != client {
		t.Error("Conn field was not passed to OnCreateStreamer")
	}
}
