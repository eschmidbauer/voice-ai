// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.
package internal_transformer_google

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	texttospeech "cloud.google.com/go/texttospeech/apiv1"
	"cloud.google.com/go/texttospeech/apiv1/texttospeechpb"
	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
	"github.com/rapidaai/pkg/commons"
	"github.com/rapidaai/pkg/utils"
	"github.com/rapidaai/protos"
)

// googleTextToSpeech is the main struct handling Google Text-to-Speech functionality.
type googleTextToSpeech struct {
	*googleOption
	mu sync.Mutex // Ensures thread-safe operations.

	ctx       context.Context
	ctxCancel context.CancelFunc

	contextId    string                                                // Tracks context ID for audio synthesis.
	logger       commons.Logger                                        // Logger for debugging and error reporting.
	client       *texttospeech.Client                                  // Google TTS client.
	streamClient texttospeechpb.TextToSpeech_StreamingSynthesizeClient // Streaming client for real-time TTS.
	onPacket     func(pkt ...internal_type.Packet) error               // Callback for handling audio packets.
	normalizer   internal_type.TextNormalizer                          // Text normalizer for preprocessing.

	// TTS latency tracking
	ttsStartedAt  time.Time
	ttsMetricSent bool
}

// Name returns the name of this transformer implementation.
func (*googleTextToSpeech) Name() string {
	return "google-text-to-speech"
}

// NewGoogleTextToSpeech creates a new instance of googleTextToSpeech.
func NewGoogleTextToSpeech(ctx context.Context, logger commons.Logger, credential *protos.VaultCredential,
	onPacket func(pkt ...internal_type.Packet) error,
	opts utils.Option) (internal_type.TextToSpeechTransformer, error) {
	// Initialize Google TTS options.
	googleOption, err := NewGoogleOption(logger, credential, opts)
	if err != nil {
		// Log and return error if initialization fails.
		logger.Errorf("intializing google failed %+v", err)
		return nil, err
	}

	// Create Google TTS client with options.
	client, err := texttospeech.NewClient(ctx, googleOption.GetClientOptions()...)
	if err != nil {
		// Log and return error if client creation fails.
		logger.Errorf("error while creating client for google tts %+v", err)
		return nil, err
	}

	xctx, contextCancel := context.WithCancel(ctx)
	// Return configured TTS instance.
	return &googleTextToSpeech{
		ctx:       xctx,
		ctxCancel: contextCancel,

		logger:       logger,
		onPacket:     onPacket,
		client:       client,
		googleOption: googleOption,
		normalizer:   NewGoogleNormalizer(logger, opts),
	}, nil
}

// Initialize sets up the streaming synthesis functionality.
func (google *googleTextToSpeech) Initialize() error {
	start := time.Now()
	// Start a streaming synthesis session.
	stream, err := google.client.StreamingSynthesize(google.ctx)
	if err != nil {
		google.logger.Errorf("failed to create bidirectional stream for google tts: %v", err)
		return fmt.Errorf("failed to create bidirectional stream: %w", err)
	}

	req := texttospeechpb.StreamingSynthesizeRequest{
		StreamingRequest: &texttospeechpb.
			StreamingSynthesizeRequest_StreamingConfig{
			StreamingConfig: google.TextToSpeechOptions(),
		},
	}

	google.mu.Lock()
	if google.streamClient != nil {
		_ = google.streamClient.CloseSend()
	}
	google.streamClient = stream
	currentContextId := google.contextId
	google.mu.Unlock()

	// Send the initial configuration request.
	if err = stream.Send(&req); err != nil {
		google.logger.Errorf("failed to initialize google text to speech: %v", err)
		return fmt.Errorf("failed to send config request: %w", err)
	}

	go google.recvLoop(stream, currentContextId)
	google.logger.Debugf("google-tts: connection established")
	google.onPacket(internal_type.ConversationEventPacket{
		Name: "tts",
		Data: map[string]string{
			"type":     "initialized",
			"provider": google.Name(),
			"init_ms":  fmt.Sprintf("%d", time.Since(start).Milliseconds()),
		},
		Time: time.Now(),
	})
	return nil
}

// Transform handles streaming synthesis requests for input text.
func (google *googleTextToSpeech) Transform(ctx context.Context, in internal_type.LLMPacket) error {
	google.mu.Lock()
	currentCtx := google.contextId
	if in.ContextId() != google.contextId {
		google.contextId = in.ContextId()
		google.ttsStartedAt = time.Time{}
		google.ttsMetricSent = false
	}
	sCli := google.streamClient
	google.mu.Unlock()
	if sCli == nil {
		return fmt.Errorf("google-tts: calling transform without initialize")
	}

	switch input := in.(type) {
	case internal_type.InterruptionPacket:
		if currentCtx != "" {
			google.mu.Lock()
			google.ttsStartedAt = time.Time{}
			google.ttsMetricSent = false
			google.mu.Unlock()
			google.onPacket(internal_type.ConversationEventPacket{
				Name: "tts",
				Data: map[string]string{"type": "interrupted"},
				Time: time.Now(),
			})
			if err := google.Initialize(); err != nil {
				return fmt.Errorf("failed to reinitialize stream on context change: %w", err)
			}
			google.mu.Lock()
			sCli = google.streamClient
			google.mu.Unlock()
		}
		return nil
	case internal_type.LLMResponseDeltaPacket:
		google.mu.Lock()
		if google.ttsStartedAt.IsZero() {
			google.ttsStartedAt = time.Now()
		}
		google.mu.Unlock()
		normalized := google.normalizer.Normalize(ctx, input.Text)
		if err := sCli.Send(&texttospeechpb.StreamingSynthesizeRequest{
			StreamingRequest: &texttospeechpb.StreamingSynthesizeRequest_Input{
				Input: &texttospeechpb.StreamingSynthesisInput{
					InputSource: &texttospeechpb.StreamingSynthesisInput_Text{Text: normalized},
				},
			},
		}); err != nil {
			google.logger.Errorf("google-tts: failed to synthesize text: %v", err)
			return fmt.Errorf("failed to synthesize text: %w", err)
		}
		google.onPacket(internal_type.ConversationEventPacket{
			Name: "tts",
			Data: map[string]string{
				"type": "speaking",
				"text": input.Text,
			},
			Time: time.Now(),
		})
		return nil
	case internal_type.LLMResponseDonePacket:
		// Signal to the server that no more input will be sent.
		// This triggers server-side EOF → recvLoop emits TextToSpeechEndPacket.
		if err := sCli.CloseSend(); err != nil {
			google.logger.Errorf("google-tts: failed to close send: %v", err)
			return fmt.Errorf("failed to close send: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("google-tts: unsupported input type %T", in)
	}
}

// recvLoop reads audio from the gRPC stream for the lifetime of the synthesis session.
// It exits when the stream ends (EOF, cancellation, or error).
func (g *googleTextToSpeech) recvLoop(streamClient texttospeechpb.TextToSpeech_StreamingSynthesizeClient, initialContextId string) {
	for {
		select {
		case <-g.ctx.Done():
			return
		default:
		}

		resp, err := streamClient.Recv()
		if err != nil {
			if err == io.EOF {
				g.mu.Lock()
				effectiveCtx := g.contextId
				if effectiveCtx == "" {
					effectiveCtx = initialContextId
				}
				g.mu.Unlock()
				g.onPacket(
					internal_type.TextToSpeechEndPacket{ContextID: effectiveCtx},
					internal_type.ConversationEventPacket{
						Name: "tts",
						Data: map[string]string{"type": "completed"},
						Time: time.Now(),
					},
				)
				return
			}
			if strings.Contains(err.Error(), "Stream aborted due to long duration elapsed without input sent") {
				g.logger.Debugf("google-tts: stream aborted due to timeout, reinitializing")
				g.mu.Lock()
				effectiveCtx := g.contextId
				if effectiveCtx == "" {
					effectiveCtx = initialContextId
				}
				g.mu.Unlock()
				g.onPacket(internal_type.TextToSpeechEndPacket{ContextID: effectiveCtx})
				go g.Initialize()
				return
			}
			g.mu.Lock()
			effectiveCtx := g.contextId
			if effectiveCtx == "" {
				effectiveCtx = initialContextId
			}
			g.mu.Unlock()
			g.onPacket(internal_type.TextToSpeechEndPacket{ContextID: effectiveCtx})
			g.logger.Errorf("google-tts: error receiving from stream: %v", err)
			return
		}

		if resp == nil {
			continue
		}

		g.mu.Lock()
		currentContextId := g.contextId
		currentStreamClient := g.streamClient
		g.mu.Unlock()

		if currentStreamClient != streamClient {
			g.logger.Debugf("google-tts: interrupted, stream replaced - stopping old callback")
			return
		}

		effectiveContextId := currentContextId
		if effectiveContextId == "" {
			effectiveContextId = initialContextId
		}

		audioContent := resp.GetAudioContent()
		g.mu.Lock()
		startedAt := g.ttsStartedAt
		metricSent := g.ttsMetricSent
		if !metricSent && !startedAt.IsZero() {
			g.ttsMetricSent = true
		}
		g.mu.Unlock()
		if !metricSent && !startedAt.IsZero() {
			g.onPacket(internal_type.MessageMetricPacket{
				ContextID: effectiveContextId,
				Metrics: []*protos.Metric{{
					Name:  "tts_latency_ms",
					Value: fmt.Sprintf("%d", time.Since(startedAt).Milliseconds()),
				}},
			})
		}
		if err := g.onPacket(internal_type.TextToSpeechAudioPacket{ContextID: effectiveContextId, AudioChunk: audioContent}); err != nil {
			g.logger.Errorf("google-tts: failed to send packet: %v", err)
		}
	}
}

// Close safely shuts down the TTS client and streaming client.
func (g *googleTextToSpeech) Close(ctx context.Context) error {
	g.ctxCancel()

	g.mu.Lock()
	defer g.mu.Unlock()
	var combinedErr error
	if g.streamClient != nil {
		// Attempt to close the streaming client.
		if err := g.streamClient.CloseSend(); err != nil {
			// Log the error if closure fails.
			combinedErr = fmt.Errorf("error closing StreamClient: %v", err)
			g.logger.Errorf(combinedErr.Error())
		}
	}

	if g.client != nil {
		// Attempt to close the client.
		if err := g.client.Close(); err != nil {
			// Log the error if closure fails.
			combinedErr = fmt.Errorf("error closing Client: %v", err)
			g.logger.Errorf(combinedErr.Error())
		}
	}
	return combinedErr
}
