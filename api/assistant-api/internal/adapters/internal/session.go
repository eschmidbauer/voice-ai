// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

// Package internal_adapter_generic provides the generic adapter implementation
// for managing voice assistant sessions. It handles the complete lifecycle of
// assistant conversations including connection, disconnection, audio streaming,
// and state management.
package adapter_internal

import (
	"context"
	"fmt"
	"sync"
	"time"

	internal_audio_recorder "github.com/rapidaai/api/assistant-api/internal/audio/recorder"
	internal_assistant_entity "github.com/rapidaai/api/assistant-api/internal/entity/assistants"
	internal_conversation_entity "github.com/rapidaai/api/assistant-api/internal/entity/conversations"
	observe "github.com/rapidaai/api/assistant-api/internal/observe"
	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
	"github.com/rapidaai/pkg/types"
	type_enums "github.com/rapidaai/pkg/types/enums"
	"github.com/rapidaai/pkg/utils"
	"github.com/rapidaai/protos"
	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// =============================================================================
// Constants
// =============================================================================

const (
	// dbWriteTimeout is the maximum duration allowed for database write operations
	// (inserts, updates, metric flushes). Uses a background context so that writes
	// are not cancelled by the caller's context lifecycle.
	dbWriteTimeout = 1 * time.Second
)

// =============================================================================
// Session Lifecycle Management
// =============================================================================

// Disconnect gracefully terminates an active assistant conversation session.
//
// This method orchestrates the complete disconnection lifecycle by:
//   - Closing all active listeners (speech-to-text transformers)
//   - Closing all active speakers (text-to-speech transformers)
//   - Flushing final conversation metrics (duration, status)
//   - Persisting audio recordings to storage
//   - Exporting telemetry data for analytics
//   - Cleaning up the assistant executor
//   - Stopping any active idle timeout timers
//
// The method executes resource cleanup operations concurrently for optimal
// performance while ensuring all operations complete before returning.
//
// Thread Safety: This method is safe to call from any goroutine, but should
// only be called once per session to avoid duplicate cleanup operations.
func (r *genericRequestor) Disconnect(ctx context.Context) {
	startTime := time.Now()

	// Phase 1: Close all session resources concurrently
	var waitGroup sync.WaitGroup
	waitGroup.Add(2)

	// Close speech-to-text listener
	utils.Go(ctx, func() {
		defer waitGroup.Done()
		if err := r.disconnectSpeechToText(ctx); err != nil {
			r.logger.Tracef(ctx, "failed to close input transformer: %+v", err)
		}

		if err := r.disconnectEndOfSpeech(ctx); err != nil {
			r.logger.Tracef(ctx, "failed to close end of speech: %+v", err)
		}

	})

	// Close text-to-speech speaker
	utils.Go(ctx, func() {
		defer waitGroup.Done()
		if err := r.disconnectTextToSpeech(ctx); err != nil {
			r.logger.Tracef(ctx, "failed to close output transformer: %+v", err)
		}

		if err := r.disconnectTextAggregator(); err != nil {
			r.logger.Tracef(ctx, "failed to close text aggregator: %+v", err)
		}
	})
	waitGroup.Wait()

	// Phase 2: Trigger end-of-conversation hooks
	r.OnEndConversation(ctx)

	// Phase 3: Persist audio recording asynchronously
	r.persistRecording(ctx)

	// Phase 4: Flush and shut down observe collectors.
	// Collect the "disconnected" event directly rather than via OnPacket/lowCh
	// because the dispatcher goroutine runs on the streamer context, which is
	// already cancelled by the time Disconnect() is called. drainChannels() will
	// have already returned, so any packet enqueued here would be silently lost.
	r.events.Collect(context.Background(), observe.EventRecord{
		MessageID: r.GetID(),
		Name:      "session",
		Data:      map[string]string{"type": "disconnected", "total_messages": fmt.Sprintf("%d", len(r.GetHistories()))},
		Time:      time.Now(),
	})
	r.shutdownCollectors(ctx)

	// Phase 5: Close assistant executor and stop timers
	r.closeExecutor(ctx)
	r.stopTimers()
	r.logger.Benchmark("session.Disconnect", time.Since(startTime))
}

// Connect establishes a new assistant session or resumes an existing one.
func (r *genericRequestor) Connect(
	ctx context.Context,
	auth types.SimplePrinciple,
	config *protos.ConversationInitialization,
) error {
	// Start four dedicated dispatchers before any OnPacket calls can happen.
	// Each channel gets its own goroutine so no priority tier can stall another.
	go r.runCriticalDispatcher(ctx) // interrupts, directives
	go r.runInputDispatcher(ctx)    // inbound: user audio, denoise, VAD, STT, EOS
	go r.runOutputDispatcher(ctx)   // outbound: LLM, text aggregator, TTS
	go r.runLowDispatcher(ctx)      // DB writes, metrics, events, tools

	r.SetAuth(auth)

	assistant, err := r.GetAssistant(ctx, auth, config.Assistant.AssistantId, config.Assistant.Version)
	if err != nil {
		r.logger.Errorf("failed to retrieve assistant configuration: %+v", err)
		return err
	}

	if conversationID := config.GetAssistantConversationId(); conversationID > 0 {
		if err := r.resumeSession(ctx, config, assistant); err != nil {
			return err
		}
	} else {
		if err := r.createSession(ctx, config, assistant); err != nil {
			return err
		}
	}
	return nil
}

// persistRecording saves the audio recording asynchronously.
func (r *genericRequestor) persistRecording(ctx context.Context) {
	if r.recorder != nil {
		utils.Go(ctx, func() {
			userAudio, systemAudio, err := r.recorder.Persist()
			if err != nil {
				r.logger.Tracef(ctx, "failed to persist audio recording: %+v", err)
				return
			}
			if err = r.CreateConversationRecording(ctx, userAudio, systemAudio); err != nil {
				r.logger.Tracef(ctx, "failed to create conversation recording record: %+v", err)
			}
		})
	}
}

// closeExecutor shuts down the assistant executor and releases its resources.
func (r *genericRequestor) closeExecutor(ctx context.Context) {
	if err := r.assistantExecutor.Close(ctx); err != nil {
		r.logger.Errorf("failed to close assistant executor: %v", err)
	}
}

// stopTimers stops all active timers (idle timeout and max session duration).
func (r *genericRequestor) stopTimers() {
	if r.idleTimeoutTimer != nil {
		r.idleTimeoutTimer.Stop()
	}
	if r.maxSessionTimer != nil {
		r.maxSessionTimer.Stop()
	}
}

// =============================================================================
// Connect Helpers
// =============================================================================

// resumeSession resumes an existing conversation session.
func (r *genericRequestor) resumeSession(
	ctx context.Context,
	config *protos.ConversationInitialization,
	assistant *internal_assistant_entity.Assistant,
) error {
	conversation, err := r.ResumeConversation(ctx, assistant, config)
	if err != nil {
		r.logger.Errorf("failed to resume conversation: %+v", err)
		return err
	}

	r.OnPacket(ctx, internal_type.ConversationEventPacket{
		Name: "session",
		Data: map[string]string{
			"type":          "resumed",
			"source":        fmt.Sprintf("%v", r.source),
			"identifier":    r.identifier(config),
			"message_count": fmt.Sprintf("%d", len(r.GetHistories())),
		},
		Time: time.Now(),
	})

	errGroup, _ := errgroup.WithContext(ctx)

	errGroup.Go(func() error {
		if err := r.assistantExecutor.Initialize(ctx, r, config); err != nil {
			r.logger.Tracef(ctx, "failed to initialize executor: %+v", err)
			return err
		}
		return nil
	})

	errGroup.Go(func() error {
		if err := r.initializeTextAggregator(ctx); err != nil {
			r.logger.Errorf("unable to initialize sentence assembler with error %v", err)
		}
		return nil
	})

	errGroup.Go(func() error {
		switch config.StreamMode {
		case protos.StreamMode_STREAM_MODE_TEXT:
			r.SwitchMode(type_enums.TextMode)
		case protos.StreamMode_STREAM_MODE_AUDIO:
			if err := r.initializeTextToSpeech(ctx); err != nil {
				r.logger.Errorf("failed to initialize text-to-speech: %v", err)
				return err
			}
			r.SwitchMode(type_enums.AudioMode)
		}
		return nil
	})

	errGroup.Go(func() error {
		switch config.StreamMode {
		case protos.StreamMode_STREAM_MODE_AUDIO:
			if err := r.initializeSpeechToText(ctx); err != nil {
				r.logger.Errorf("failed to initialize speech-to-text: %v", err)
				return err
			}
		}
		return nil
	})

	r.initSessionBackground(ctx, false)

	if err = errGroup.Wait(); err != nil {
		r.notifyInitializationError(ctx, conversation.Id, err)
		return err
	}
	r.notifyConfiguration(ctx, config, conversation, assistant)
	r.initializeBehavior(ctx)
	return nil
}

// createSession creates a new conversation session.
func (r *genericRequestor) createSession(
	ctx context.Context,
	config *protos.ConversationInitialization,
	assistant *internal_assistant_entity.Assistant,
) error {
	conversation, err := r.BeginConversation(
		ctx,
		assistant,
		type_enums.DIRECTION_INBOUND,
		config,
	)
	if err != nil {
		r.logger.Errorf("failed to begin conversation: %+v", err)
		return err
	}

	r.OnPacket(ctx, internal_type.ConversationEventPacket{
		Name: "session",
		Data: map[string]string{
			"type":       "connected",
			"source":     fmt.Sprintf("%v", r.source),
			"is_new":     "true",
			"identifier": r.identifier(config),
		},
		Time: time.Now(),
	})

	errGroup, _ := errgroup.WithContext(ctx)

	errGroup.Go(func() error {
		if err := r.assistantExecutor.Initialize(ctx, r, config); err != nil {
			r.logger.Tracef(ctx, "failed to initialize executor: %+v", err)
			return err
		}
		return nil
	})

	errGroup.Go(func() error {
		switch config.StreamMode {
		case protos.StreamMode_STREAM_MODE_TEXT:
			r.SwitchMode(type_enums.TextMode)
		case protos.StreamMode_STREAM_MODE_AUDIO:
			if err := r.initializeTextToSpeech(ctx); err != nil {
				r.logger.Errorf("failed to initialize text-to-speech: %v", err)
				return err
			}
			r.SwitchMode(type_enums.AudioMode)
		}
		return nil
	})

	errGroup.Go(func() error {
		if err := r.initializeTextAggregator(ctx); err != nil {
			r.logger.Errorf("unable to initialize sentence assembler with error %v", err)
		}
		return nil
	})

	errGroup.Go(func() error {
		switch config.StreamMode {
		case protos.StreamMode_STREAM_MODE_AUDIO:
			if err := r.initializeSpeechToText(ctx); err != nil {
				r.logger.Errorf("failed to initialize speech-to-text: %v", err)
				return err
			}
		}
		return nil
	})
	r.initSessionBackground(ctx, true)
	if err = errGroup.Wait(); err != nil {
		r.notifyInitializationError(ctx, conversation.Id, err)
		return err
	}
	r.notifyConfiguration(ctx, config, conversation, assistant)
	r.initializeBehavior(ctx)
	return nil
}

// initSessionBackground launches non-critical background tasks common to both
// new and resumed sessions. isNew distinguishes which lifecycle hook to fire.
func (r *genericRequestor) initSessionBackground(ctx context.Context, isNew bool) {
	// Initialize telemetry collectors in the background so that DB lookups,
	// vault credential resolution, and OTLP connection setup do not add
	// latency to the connect path. The no-op collectors set in
	// NewGenericRequestor safely absorb any events/metrics collected before
	// the real exporters are ready.
	utils.Go(ctx, func() {
		r.initializeCollectors(ctx)
	})

	utils.Go(ctx, func() {
		rc, err := internal_audio_recorder.GetRecorder(r.logger)
		if err != nil {
			r.logger.Tracef(ctx, "failed to initialize audio recorder: %+v", err)
			return
		}
		r.recorder = rc
		r.recorder.Start()
		r.OnPacket(ctx, internal_type.ConversationEventPacket{
			Name: "session",
			Data: map[string]string{"type": "recording_started"},
			Time: time.Now(),
		})
	})

	utils.Go(ctx, func() {
		if err := r.initializeEndOfSpeech(ctx); err != nil {
			r.logger.Tracef(ctx, "failed to initialize input: %+v", err)
		}
	})

	utils.Go(ctx, func() {
		metrics := []*protos.Metric{{
			Name:        type_enums.STATUS.String(),
			Value:       type_enums.RECORD_IN_PROGRESS.String(),
			Description: "Conversation is currently in progress",
		}}
		r.onAddMetrics(ctx, metrics...)
		r.metrics.Collect(ctx, observe.ConversationMetricRecord{
			ConversationID: fmt.Sprintf("%d", r.Conversation().Id),
			Metrics:        metrics,
			Time:           time.Now(),
		})
	})

	utils.Go(ctx, func() {
		r.storeClientInformation(ctx)
	})

	if isNew {
		utils.Go(ctx, func() {
			if err := r.OnBeginConversation(ctx); err != nil {
				r.logger.Errorf("failed to execute begin conversation hooks: %+v", err)
			}
		})
	} else {
		utils.Go(ctx, func() {
			if err := r.OnResumeConversation(ctx); err != nil {
				r.logger.Errorf("failed to execute resume conversation hooks: %v", err)
			}
		})
	}
}

// notifyConfiguration sends the initial conversation configuration to the client.
func (r *genericRequestor) notifyConfiguration(
	ctx context.Context,
	config *protos.ConversationInitialization,
	conversation *internal_conversation_entity.AssistantConversation,
	assistant *internal_assistant_entity.Assistant,
) {
	if err := r.Notify(ctx, &protos.ConversationInitialization{
		AssistantConversationId: conversation.Id,
		Assistant: &protos.AssistantDefinition{
			AssistantId: assistant.Id,
			Version:     utils.GetVersionString(assistant.AssistantProviderId),
		},
		Args:         config.GetArgs(),
		Metadata:     config.GetOptions(),
		Options:      config.GetMetadata(),
		StreamMode:   config.GetStreamMode(),
		UserIdentity: config.GetUserIdentity(),
		Time:         timestamppb.Now(),
	}); err != nil {
		r.logger.Errorf("failed to send configuration notification: %v", err)
	}
}

// notifyInitializationError sends a ConversationError to the client when a core
// system (STT, TTS, executor) fails to initialize. The client receives a
// structured error message before the session is torn down, rather than a
// silent gRPC status error.
func (r *genericRequestor) notifyInitializationError(ctx context.Context, conversationId uint64, initErr error) {
	_ = r.Notify(ctx, &protos.ConversationError{
		AssistantConversationId: conversationId,
		Message:                 fmt.Sprintf("session initialization failed: %v", initErr),
	})
}

// storeClientInformation extracts client metadata from the gRPC context
// and persists it as conversation metadata for analytics purposes.
func (r *genericRequestor) storeClientInformation(ctx context.Context) {
	clientInfo := types.GetClientInfoFromGrpcContext(ctx)
	if clientInfo == nil {
		return
	}

	// Flatten client info into metadata with "client." prefix (same pattern
	// as telephony.toPhone, telephony.fromPhone). This makes fields like
	// timezone, platform, language directly available in prompt context via
	// r.metadata["client.timezone"] etc.
	flat := map[string]interface{}{}
	if clientInfo.Timezone != "" {
		flat["client.timezone"] = clientInfo.Timezone
	}
	if clientInfo.Platform != "" {
		flat["client.platform"] = clientInfo.Platform
	}
	if clientInfo.Language != "" {
		flat["client.language"] = clientInfo.Language
	}
	if clientInfo.UserAgent != "" {
		flat["client.user_agent"] = clientInfo.UserAgent
	}
	if clientInfo.Referrer != "" {
		flat["client.referrer"] = clientInfo.Referrer
	}
	if clientInfo.ConnectionType != "" {
		flat["client.connection_type"] = clientInfo.ConnectionType
	}
	if clientInfo.Latitude != 0 || clientInfo.Longitude != 0 {
		flat["client.latitude"] = fmt.Sprintf("%f", clientInfo.Latitude)
		flat["client.longitude"] = fmt.Sprintf("%f", clientInfo.Longitude)
	}

	r.onSetMetadata(ctx, r.Auth(), flat)
}
