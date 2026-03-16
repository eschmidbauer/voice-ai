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

	speech "cloud.google.com/go/speech/apiv2"
	"cloud.google.com/go/speech/apiv2/speechpb"
	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
	"github.com/rapidaai/pkg/commons"
	"github.com/rapidaai/pkg/utils"
	"github.com/rapidaai/protos"
)

type googleSpeechToText struct {
	*googleOption
	mu sync.Mutex

	logger commons.Logger

	client   *speech.Client
	stream   speechpb.Speech_StreamingRecognizeClient
	onPacket func(pkt ...internal_type.Packet) error

	// context management
	ctx       context.Context
	ctxCancel context.CancelFunc

	// observability: time when speech started
	startedAt time.Time
}

// Name implements internal_transformer.SpeechToTextTransformer.
func (g *googleSpeechToText) Name() string {
	return "google-speech-to-text"
}

func NewGoogleSpeechToText(ctx context.Context, logger commons.Logger, credential *protos.VaultCredential,
	onPacket func(pkt ...internal_type.Packet) error,
	opts utils.Option,
) (internal_type.SpeechToTextTransformer, error) {
	start := time.Now()
	googleOption, err := NewGoogleOption(logger, credential, opts)
	if err != nil {
		logger.Errorf("google-stt: Error while GoogleOption err: %v", err)
		return nil, err
	}
	client, err := speech.NewClient(ctx, googleOption.GetSpeechToTextClientOptions()...)

	if err != nil {
		logger.Errorf("google-stt: Error creating Google client: %v", err)
		return nil, err
	}

	xctx, contextCancel := context.WithCancel(ctx)
	// Context for callback management
	logger.Benchmark("google.NewGoogleSpeechToText", time.Since(start))
	return &googleSpeechToText{
		ctx:          xctx,
		ctxCancel:    contextCancel,
		logger:       logger,
		client:       client,
		googleOption: googleOption,
		onPacket:     onPacket,
	}, nil
}

// Transform implements internal_transformer.SpeechToTextTransformer.
func (google *googleSpeechToText) Transform(c context.Context, in internal_type.UserAudioPacket) error {
	google.mu.Lock()
	strm := google.stream
	if google.startedAt.IsZero() {
		google.startedAt = time.Now()
	}
	google.mu.Unlock()

	// If the stream was lost (e.g. Google timed out waiting for audio during
	// slow boot), re-establish it transparently before sending audio.
	if strm == nil {
		google.logger.Infof("google-stt: stream not available, re-initializing")
		if err := google.Initialize(); err != nil {
			return fmt.Errorf("google-stt: re-initialize failed: %w", err)
		}
		google.mu.Lock()
		strm = google.stream
		google.mu.Unlock()
		if strm == nil {
			return fmt.Errorf("google-stt: stream not initialized after re-initialize")
		}
	}

	return strm.Send(&speechpb.StreamingRecognizeRequest{
		StreamingRequest: &speechpb.StreamingRecognizeRequest_Audio{
			Audio: in.Audio,
		},
	})
}

// recvLoop reads responses from the gRPC stream for the lifetime of the STT session.
// It exits when the stream ends (EOF, cancellation, or error).
func (g *googleSpeechToText) recvLoop(stream speechpb.Speech_StreamingRecognizeClient) {
	for {
		select {
		case <-g.ctx.Done():
			return
		default:
		}

		resp, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				return
			}
			g.logger.Errorf("google-stt: recv error: %v", err)
			// Mark stream as dead so Transform() can re-initialize on next audio packet.
			g.mu.Lock()
			g.stream = nil
			g.mu.Unlock()
			g.onPacket(internal_type.ConversationEventPacket{
				Name: "stt",
				Data: map[string]string{"type": "error", "error": err.Error()},
				Time: time.Now(),
			})
			return
		}
		if resp == nil {
			continue
		}

		for _, result := range resp.Results {
			if len(result.Alternatives) == 0 {
				continue
			}
			alt := result.Alternatives[0]
			if len(alt.GetTranscript()) == 0 {
				continue
			}
			confStr := fmt.Sprintf("%.4f", float64(alt.GetConfidence()))
			transcript := alt.GetTranscript()

			if v, err := g.mdlOpts.GetFloat64("listen.threshold"); err == nil {
				if alt.GetConfidence() < float32(v) {
					g.onPacket(
						internal_type.ConversationEventPacket{
							Name: "stt",
							Data: map[string]string{
								"type":       "low_confidence",
								"script":     transcript,
								"confidence": confStr,
								"threshold":  fmt.Sprintf("%.4f", v),
							},
							Time: time.Now(),
						},
					)
					continue
				}
			}
			if result.GetIsFinal() {
				now := time.Now()
				var latencyMs int64
				g.mu.Lock()
				if !g.startedAt.IsZero() {
					latencyMs = now.Sub(g.startedAt).Milliseconds()
					g.startedAt = time.Time{}
				}
				g.mu.Unlock()
				g.onPacket(
					internal_type.InterruptionPacket{Source: internal_type.InterruptionSourceWord},
					internal_type.SpeechToTextPacket{
						Script:     transcript,
						Confidence: float64(alt.GetConfidence()),
						Language:   result.GetLanguageCode(),
						Interim:    false,
					},
					internal_type.ConversationEventPacket{
						Name: "stt",
						Data: map[string]string{
							"type":       "completed",
							"script":     transcript,
							"confidence": confStr,
							"language":   result.GetLanguageCode(),
							"word_count": fmt.Sprintf("%d", len(strings.Fields(transcript))),
							"char_count": fmt.Sprintf("%d", len(transcript)),
						},
						Time: now,
					},
					internal_type.MessageMetricPacket{
						Metrics: []*protos.Metric{{Name: "stt_latency_ms", Value: fmt.Sprintf("%d", latencyMs)}},
					},
				)
			} else {
				g.onPacket(
					internal_type.InterruptionPacket{Source: internal_type.InterruptionSourceWord},
					internal_type.SpeechToTextPacket{
						Script:     transcript,
						Confidence: float64(result.GetStability()),
						Language:   result.GetLanguageCode(),
						Interim:    true,
					},
					internal_type.ConversationEventPacket{
						Name: "stt",
						Data: map[string]string{
							"type":       "interim",
							"script":     transcript,
							"confidence": confStr,
						},
						Time: time.Now(),
					},
				)
			}
		}
	}
}

func (google *googleSpeechToText) Initialize() error {
	start := time.Now()
	stream, err := google.client.StreamingRecognize(google.ctx)
	if err != nil {
		google.logger.Errorf("google-stt: error creating google-stt stream: %v", err)
		return err
	}

	google.mu.Lock()
	if google.stream != nil {
		_ = google.stream.CloseSend()
	}
	google.stream = stream
	google.mu.Unlock()

	if err := stream.Send(&speechpb.StreamingRecognizeRequest{
		Recognizer: google.GetRecognizer(),
		StreamingRequest: &speechpb.StreamingRecognizeRequest_StreamingConfig{
			StreamingConfig: google.SpeechToTextOptions(),
		},
	}); err != nil {
		google.logger.Errorf("google-stt: error creating google-stt stream: %v", err)
		return err
	}

	go google.recvLoop(stream)
	google.logger.Debugf("google-stt: connection established")
	google.onPacket(internal_type.ConversationEventPacket{
		Name: "stt",
		Data: map[string]string{
			"type":     "initialized",
			"provider": google.Name(),
			"init_ms":  fmt.Sprintf("%d", time.Since(start).Milliseconds()),
		},
		Time: time.Now(),
	})
	return nil
}

func (g *googleSpeechToText) Close(ctx context.Context) error {
	g.ctxCancel()

	g.mu.Lock()
	defer g.mu.Unlock()

	var combinedErr error
	if g.stream != nil {
		if err := g.stream.CloseSend(); err != nil {
			combinedErr = fmt.Errorf("error closing StreamClient: %v", err)
			g.logger.Errorf(combinedErr.Error())
		}
	}

	if g.client != nil {
		if err := g.client.Close(); err != nil {
			// Log the error if closure fails.
			combinedErr = fmt.Errorf("error closing Client: %v", err)
			g.logger.Errorf(combinedErr.Error())
		}
	}
	return combinedErr
}
