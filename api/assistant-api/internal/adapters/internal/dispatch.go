// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.
package adapter_internal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	internal_audio "github.com/rapidaai/api/assistant-api/internal/audio"
	observe "github.com/rapidaai/api/assistant-api/internal/observe"
	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
	type_enums "github.com/rapidaai/pkg/types/enums"
	"github.com/rapidaai/pkg/utils"
	"github.com/rapidaai/protos"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// =============================================================================
// OnPacket — enqueue into the priority channel
// =============================================================================

// OnPacket enqueues each packet into the appropriate priority channel.
// Each channel has a dedicated dispatcher goroutine so no tier can stall another.
//
// Priority:
//
//	critical — interrupts, directives                        (preempts everything)
//	input    — inbound audio, denoise, VAD, STT, EOS         (user → system)
//	output   — LLM generation, text aggregation, TTS          (system → user)
//	low      — recording, metrics, persistence, events         (background work)
func (r *genericRequestor) OnPacket(ctx context.Context, pkts ...internal_type.Packet) error {
	for _, p := range pkts {
		e := packetEnvelope{ctx: ctx, pkt: p}
		switch p.(type) {
		// Critical — interrupts, tool lifecycle
		case internal_type.InterruptionDetectedPacket,
			internal_type.InterruptTTSPacket,
			internal_type.InterruptLLMPacket,
			internal_type.TurnChangePacket,
			internal_type.LLMToolCallPacket,
			internal_type.LLMToolResultPacket:
			r.criticalCh <- e

		// Input — inbound audio pipeline, VAD, STT, EOS
		case internal_type.UserAudioReceivedPacket,
			internal_type.UserTextReceivedPacket,
			internal_type.DenoiseAudioPacket,
			internal_type.DenoisedAudioPacket,
			internal_type.VadAudioPacket,
			internal_type.VadSpeechActivityPacket,
			internal_type.SpeechToTextPacket,
			internal_type.EndOfSpeechPacket,
			internal_type.InterimEndOfSpeechPacket,
			internal_type.NormalizeInputPacket,
			internal_type.NormalizedUserTextPacket:
			r.inputCh <- e

		// Output — LLM generation, TTS, outbound pipeline
		case internal_type.ExecuteLLMPacket,
			internal_type.LLMResponseDeltaPacket,
			internal_type.LLMResponseDonePacket,
			internal_type.LLMErrorPacket,
			internal_type.AggregateTextPacket,
			internal_type.InjectMessagePacket,
			internal_type.SpeakTextPacket,
			internal_type.TextToSpeechAudioPacket,
			internal_type.TextToSpeechEndPacket:
			r.outputCh <- e

		// Low — recording, metrics, persistence, events
		case internal_type.RecordUserAudioPacket,
			internal_type.RecordAssistantAudioPacket,
			internal_type.SaveMessagePacket,
			internal_type.ConversationMetricPacket,
			internal_type.ConversationMetadataPacket,
			internal_type.AssistantMessageMetricPacket,
			internal_type.UserMessageMetricPacket,
			internal_type.UserMessageMetadataPacket,
			internal_type.AssistantMessageMetadataPacket,
			internal_type.ConversationEventPacket:
			r.lowCh <- e

		default:
			r.logger.Warnf("OnPacket: unrouted packet type %T, falling back to inputCh", p)
			r.inputCh <- e
		}
	}
	return nil
}

// =============================================================================
// runDispatcher — dedicated consumer goroutines per priority tier
// =============================================================================

// runCriticalDispatcher processes critical-priority packets (interrupts,
// directives) on a dedicated goroutine so they are never queued behind
// normal or low-priority work.
func (r *genericRequestor) runCriticalDispatcher(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			r.drainCriticalChannel()
			return
		case e := <-r.criticalCh:
			r.dispatch(e.ctx, e.pkt)
		}
	}
}

func (r *genericRequestor) drainCriticalChannel() {
	for {
		select {
		case e := <-r.criticalCh:
			r.dispatch(e.ctx, e.pkt)
		default:
			return
		}
	}
}

// runInputDispatcher processes inbound pipeline packets (user audio, denoise,
// VAD, STT, EOS) on a dedicated goroutine. Kept separate from the output
// pipeline so a burst of inbound audio never delays LLM/TTS streaming.
func (r *genericRequestor) runInputDispatcher(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			r.drainInputChannel()
			return
		case e := <-r.inputCh:
			r.dispatch(e.ctx, e.pkt)
		}
	}
}

func (r *genericRequestor) drainInputChannel() {
	for {
		select {
		case e := <-r.inputCh:
			r.dispatch(e.ctx, e.pkt)
		default:
			return
		}
	}
}

// runOutputDispatcher processes outbound pipeline packets (LLM generation,
// text aggregation, TTS audio) on a dedicated goroutine. Kept separate from
// the input pipeline so LLM response streaming is never queued behind audio.
func (r *genericRequestor) runOutputDispatcher(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			r.drainOutputChannel()
			return
		case e := <-r.outputCh:
			r.dispatch(e.ctx, e.pkt)
		}
	}
}

func (r *genericRequestor) drainOutputChannel() {
	for {
		select {
		case e := <-r.outputCh:
			r.dispatch(e.ctx, e.pkt)
		default:
			return
		}
	}
}

// runLowDispatcher processes low-priority packets (persistence, metrics,
// events, tools) on a dedicated goroutine so DB writes never stall audio
// or LLM processing.
func (r *genericRequestor) runLowDispatcher(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			// Drain remaining low packets so completion metrics (STATUS,
			// TIME_TAKEN) enqueued just before Disconnect() are not lost.
			r.drainLowChannel()
			return
		case e := <-r.lowCh:
			r.dispatch(e.ctx, e.pkt)
		}
	}
}

func (r *genericRequestor) drainLowChannel() {
	for {
		select {
		case e := <-r.lowCh:
			r.dispatch(e.ctx, e.pkt)
		default:
			return
		}
	}
}

// =============================================================================
// dispatch — routes a single packet to its handler
// =============================================================================

func (r *genericRequestor) dispatch(ctx context.Context, p internal_type.Packet) {
	switch vl := p.(type) {
	case internal_type.UserTextReceivedPacket:
		r.handleUserText(ctx, vl)
	case internal_type.UserAudioReceivedPacket:
		r.handleUserAudio(ctx, vl)
	case internal_type.DenoiseAudioPacket:
		r.handleDenoise(ctx, vl)
	case internal_type.DenoisedAudioPacket:
		r.handleDenoisedAudio(ctx, vl)
	case internal_type.VadAudioPacket:
		r.handleVadAudio(ctx, vl)
	case internal_type.VadSpeechActivityPacket:
		r.callEndOfSpeech(ctx, vl)
	case internal_type.RecordUserAudioPacket:
		r.handleRecordUserAudio(ctx, vl)
	case internal_type.RecordAssistantAudioPacket:
		r.handleRecordAssistantAudio(ctx, vl)
	case internal_type.SpeechToTextPacket:
		r.handleSpeechToText(ctx, vl)

		// End of speech and normalization
	case internal_type.InterimEndOfSpeechPacket:
		r.handleInterimEndOfSpeech(ctx, vl)
	case internal_type.EndOfSpeechPacket:
		r.handleEndOfSpeech(ctx, vl)
	case internal_type.NormalizeInputPacket:
		r.handleNormalizeInput(ctx, vl)
	case internal_type.NormalizedUserTextPacket:
		r.handleNormalizedText(ctx, vl)

		// Interruptions
	case internal_type.InterruptionDetectedPacket:
		r.handleInterruption(ctx, vl)
	case internal_type.InterruptTTSPacket:
		r.handleInterruptTTS(ctx, vl)
	case internal_type.InterruptLLMPacket:
		r.handleInterruptLLM(ctx, vl)
	case internal_type.TurnChangePacket:
		r.handleContextChange(ctx, vl)

		// LLM pipeline
	case internal_type.ExecuteLLMPacket:
		r.handleExecuteLLM(ctx, vl)
	case internal_type.LLMResponseDeltaPacket:
		r.handleLLMDelta(ctx, vl)
	case internal_type.LLMResponseDonePacket:
		r.handleLLMDone(ctx, vl)
	case internal_type.LLMErrorPacket:
		r.handleLLMError(ctx, vl)

		// Text aggregation
	case internal_type.AggregateTextPacket:
		r.handleAggregateText(ctx, vl)

		// Static / system-injected
	case internal_type.InjectMessagePacket:
		r.handleInjectMessagePacket(ctx, vl)
	case internal_type.SpeakTextPacket:
		r.handleSpeakText(ctx, vl)
	case internal_type.TextToSpeechAudioPacket:
		r.handleTTSAudio(ctx, vl)
	case internal_type.TextToSpeechEndPacket:
		r.handleTTSEnd(ctx, vl)
	case internal_type.SaveMessagePacket:
		r.handleSaveMessage(ctx, vl)
	case internal_type.ConversationMetricPacket:
		r.handleConversationMetric(ctx, vl)
	case internal_type.ConversationMetadataPacket:
		r.handleConversationMetadata(ctx, vl)
	case internal_type.UserMessageMetricPacket:
		r.handleUserMessageMetric(ctx, vl)
	case internal_type.AssistantMessageMetricPacket:
		r.handleAssistantMessageMetric(ctx, vl)
	case internal_type.UserMessageMetadataPacket:
		r.handleUserMessageMetadata(ctx, vl)
	case internal_type.AssistantMessageMetadataPacket:
		r.handleAssistantMessageMetadata(ctx, vl)
	case internal_type.LLMToolCallPacket:
		r.handleToolCall(ctx, vl)
	case internal_type.LLMToolResultPacket:
		r.handleToolResult(ctx, vl)
	case internal_type.ConversationEventPacket:
		r.handleConversationEvent(ctx, vl)
	default:
		r.logger.Warnf("unknown packet type received in dispatcher %T", vl)
	}
}

// =============================================================================
// calling end of speech and handling
// =============================================================================

func (talking *genericRequestor) callEndOfSpeech(ctx context.Context, vl internal_type.Packet) error {
	if talking.endOfSpeech != nil {
		utils.Go(ctx, func() {
			if err := talking.endOfSpeech.Analyze(ctx, vl); err != nil {
				talking.logger.Errorf("end of speech analyze error: %v", err)
			}
		})
		return nil
	}
	return errors.New("end of speech analyzer not configured")
}

func (talking *genericRequestor) handleEndOfSpeech(ctx context.Context, vl internal_type.EndOfSpeechPacket) {
	talking.OnPacket(ctx, internal_type.NormalizeInputPacket{
		ContextID: talking.GetID(),
		Speech:    vl.Speech,
		Speechs:   vl.Speechs,
	})
}

func (talking *genericRequestor) handleNormalizeInput(ctx context.Context, vl internal_type.NormalizeInputPacket) {
	eos := internal_type.EndOfSpeechPacket{ContextID: vl.ContextID, Speech: vl.Speech, Speechs: vl.Speechs}
	if err := talking.callInputNormalizer(ctx, eos); err != nil {
		talking.OnPacket(ctx, internal_type.NormalizedUserTextPacket{
			ContextID: vl.ContextID,
			Text:      vl.Speech,
		})
	}
}

func (talking *genericRequestor) handleInterimEndOfSpeech(ctx context.Context, vl internal_type.InterimEndOfSpeechPacket) {
	talking.Notify(ctx, &protos.ConversationUserMessage{
		Id:        talking.GetID(),
		Message:   &protos.ConversationUserMessage_Text{Text: vl.Speech},
		Completed: false,
		Time:      timestamppb.New(time.Now()),
	})
}

// =============================================================================
// Text aggregation handler
// =============================================================================

// handleAggregateText passes validated LLM output through the text aggregator.
// The aggregator batches deltas into sentence-sized chunks before emitting
// SpeakTextPacket. If no aggregator is configured, falls back to direct emit.
func (talking *genericRequestor) handleAggregateText(ctx context.Context, vl internal_type.AggregateTextPacket) {
	if talking.textAggregator != nil {
		// The aggregator expects LLMResponseDelta/DonePacket — convert back.
		var pkt internal_type.Packet
		if vl.IsFinal {
			pkt = internal_type.LLMResponseDonePacket{ContextID: vl.ContextID, Text: vl.Text}
		} else {
			pkt = internal_type.LLMResponseDeltaPacket{ContextID: vl.ContextID, Text: vl.Text}
		}
		if err := talking.textAggregator.Aggregate(ctx, pkt); err == nil {
			return
		}
	}
	// Fallback: no aggregator or aggregation error — emit SpeakTextPacket directly.
	talking.OnPacket(ctx, internal_type.SpeakTextPacket{ContextID: vl.ContextID, Text: vl.Text, IsFinal: vl.IsFinal})
}

func (talking *genericRequestor) callSpeechToText(ctx context.Context, vl internal_type.UserAudioReceivedPacket) error {
	if talking.speechToTextTransformer != nil {
		utils.Go(ctx, func() {
			if err := talking.speechToTextTransformer.Transform(ctx, vl); err != nil {
				talking.logger.Tracef(ctx, "error while transforming input %s and error %s", talking.speechToTextTransformer.Name(), err.Error())
			}
		})
	}
	return nil
}

// =============================================================================
// Input handlers
// =============================================================================

func (talking *genericRequestor) handleUserText(ctx context.Context, vl internal_type.UserTextReceivedPacket) {
	// Cancel any in-flight LLM/TTS before starting the new text turn.
	// On the first message (state Unknown) Transition(Interrupted) is
	// rejected by the state machine and handleInterruption returns early —
	// which is correct: there is nothing active to interrupt.
	talking.handleInterruption(ctx, internal_type.InterruptionDetectedPacket{
		ContextID: talking.GetID(),
		Source:    internal_type.InterruptionSourceWord,
	})

	vl.ContextID = talking.GetID()
	if err := talking.callEndOfSpeech(ctx, vl); err != nil {
		talking.OnPacket(ctx, internal_type.EndOfSpeechPacket{ContextID: vl.ContextID, Speech: vl.Text})
	}
}

func (talking *genericRequestor) handleUserAudio(ctx context.Context, vl internal_type.UserAudioReceivedPacket) {
	if talking.denoiser != nil && !vl.NoiseReduced {
		talking.OnPacket(ctx, internal_type.DenoiseAudioPacket{ContextID: vl.ContextID, Audio: vl.Audio})
		return
	}
	talking.OnPacket(ctx,
		internal_type.RecordUserAudioPacket{ContextID: vl.ContextID, Audio: vl.Audio},
		internal_type.VadAudioPacket{ContextID: vl.ContextID, Audio: vl.Audio},
	)
	talking.callSpeechToText(ctx, vl)
	// Route audio to EOS for audio-based turn detectors (e.g. Pipecat Smart Turn, later other end of speech may need audio with conversation context).
	// Text-based and silence-based EOS implementations ignore this packet type.
	talking.callEndOfSpeech(ctx, vl)
}

// =============================================================================
// Pre-processing handlers
// =============================================================================

func (talking *genericRequestor) handleDenoise(ctx context.Context, vl internal_type.DenoiseAudioPacket) {
	if talking.denoiser != nil {
		if err := talking.denoiser.Denoise(ctx, vl); err != nil {
			talking.logger.Warnf("denoiser returned unexpected error: %+v", err)
		}
	}
}

func (talking *genericRequestor) handleDenoisedAudio(ctx context.Context, vl internal_type.DenoisedAudioPacket) {
	// Re-emit as UserAudioReceivedPacket with NoiseReduced=true so it flows
	// through handleUserAudio's normal fan-out (Record, VAD, STT, EOS).
	// Always mark NoiseReduced=true to prevent re-entering the denoiser,
	// even if the denoiser fell back to the original audio on error.
	talking.OnPacket(ctx, internal_type.UserAudioReceivedPacket{
		ContextID:    vl.ContextID,
		Audio:        vl.Audio,
		NoiseReduced: true,
	})
}

func (talking *genericRequestor) handleVadAudio(ctx context.Context, vl internal_type.VadAudioPacket) {
	if talking.vad != nil {
		utils.Go(ctx, func() {
			if err := talking.vad.Process(ctx, internal_type.UserAudioReceivedPacket{ContextID: vl.ContextID, Audio: vl.Audio}); err != nil {
				talking.logger.Warnf("error while processing with vad %s", err.Error())
			}
		})
	}
}

// =============================================================================
// Recording handlers
// =============================================================================

func (talking *genericRequestor) handleRecordUserAudio(ctx context.Context, vl internal_type.RecordUserAudioPacket) {
	if talking.recorder != nil {
		if err := talking.recorder.Record(ctx, vl); err != nil {
			talking.logger.Errorf("recorder error: %v", err)
		}
	}
}

func (talking *genericRequestor) handleRecordAssistantAudio(ctx context.Context, vl internal_type.RecordAssistantAudioPacket) {
	if talking.recorder != nil {
		if err := talking.recorder.Record(ctx, vl); err != nil {
			talking.logger.Errorf("recorder error: %v", err)
		}
	}
}

// =============================================================================
// Speech detection handlers
// =============================================================================

func (talking *genericRequestor) handleSpeechToText(ctx context.Context, vl internal_type.SpeechToTextPacket) {
	vl.ContextID = talking.GetID()
	if err := talking.callEndOfSpeech(ctx, vl); err != nil {
		if !vl.Interim {
			talking.OnPacket(ctx, internal_type.EndOfSpeechPacket{
				ContextID: vl.ContextID,
				Speech:    vl.Script,
				Speechs:   []internal_type.SpeechToTextPacket{vl},
			})
		}
	}
}

func (talking *genericRequestor) callInputNormalizer(ctx context.Context, vl internal_type.EndOfSpeechPacket) error {
	if talking.normalizer == nil {
		return errors.New("input normalizer not configured")
	}
	if err := talking.normalizer.Normalize(ctx, vl); err != nil {
		talking.logger.Errorf("input normalizer error: %v", err)
		return err
	}
	return nil
}

func (talking *genericRequestor) handleNormalizedText(ctx context.Context, vl internal_type.NormalizedUserTextPacket) {
	talking.stopIdleTimeoutTimerAndResetCount()
	if err := talking.Transition(LLMGenerating); err != nil {
		talking.logger.Errorf("messaging transition error: %v", err)
	}

	// Use the CURRENT session context, not the packet's carried context.
	// A word-interrupt on the critical dispatcher may have rotated the
	// context between when the EOS/normalizer pipeline started and now.
	// Using the stale packet context would cause all LLM responses to be
	// discarded as "stale_context" by handleLLMDelta/handleLLMDone.
	contextID := talking.GetID()
	vl.ContextID = contextID

	if err := talking.Notify(ctx, &protos.ConversationUserMessage{
		Id:        contextID,
		Message:   &protos.ConversationUserMessage_Text{Text: vl.Text},
		Completed: true,
		Time:      timestamppb.New(time.Now()),
	}); err != nil {
		talking.logger.Tracef(ctx, "might be returning processing the duplicate message so cut it out.")
		return
	}
	talking.OnPacket(ctx,
		internal_type.SaveMessagePacket{ContextID: contextID, MessageRole: "user", Text: vl.Text},
		internal_type.UserMessageMetadataPacket{ContextID: contextID, Metadata: []*protos.Metadata{
			{
				Key:   "language",
				Value: vl.Language.Name,
			},
			{
				Key:   "language_code",
				Value: vl.Language.ISO639_1,
			}}},
		internal_type.UserMessageMetricPacket{ContextID: contextID, Metrics: []*protos.Metric{{Name: "user_turn", Value: type_enums.CONVERSATION_COMPLETE.String(), Description: "User turn started"}}},
		internal_type.ExecuteLLMPacket{ContextID: contextID, Input: vl.Text, Normalized: vl})
}

// =============================================================================
// Interruption handlers
// =============================================================================

func (talking *genericRequestor) handleInterruption(ctx context.Context, vl internal_type.InterruptionDetectedPacket) {
	if vl.ContextID == "" {
		vl.ContextID = talking.GetID()
	}
	if talking.speechToTextTransformer != nil {
		if err := talking.speechToTextTransformer.Transform(ctx, vl); err != nil {
			talking.logger.Errorf("stt interruption update failed: %v", err)
		}
	}

	switch vl.Source {
	case internal_type.InterruptionSourceWord:
		talking.stopIdleTimeoutTimer()
		if err := talking.callEndOfSpeech(ctx, vl); err != nil {
			talking.logger.Errorf("end of speech error: %v", err)
		}
		if err := talking.Transition(Interrupted); err != nil {
			return
		}
		talking.OnPacket(ctx,
			internal_type.RecordAssistantAudioPacket{ContextID: vl.ContextID, Truncate: true},
			internal_type.InterruptTTSPacket{ContextID: vl.ContextID, StartAt: vl.StartAt, EndAt: vl.EndAt},
			internal_type.InterruptLLMPacket{ContextID: vl.ContextID},
		)
		utils.Go(ctx, func() {
			talking.Notify(ctx, &protos.ConversationInterruption{
				Type: protos.ConversationInterruption_INTERRUPTION_TYPE_WORD,
				Time: timestamppb.Now(),
			})
		})

	default:
		if vl.StartAt < 5 {
			return
		}
		if err := talking.callEndOfSpeech(ctx, vl); err != nil {
			talking.logger.Errorf("end of speech error: %v", err)
		}

		if err := talking.Transition(Interrupt); err != nil {
			return
		}

		utils.Go(ctx, func() {
			talking.Notify(ctx, &protos.ConversationInterruption{
				Type: protos.ConversationInterruption_INTERRUPTION_TYPE_VAD,
				Time: timestamppb.Now(),
			})
		})
	}
}

func (talking *genericRequestor) handleContextChange(ctx context.Context, vl internal_type.TurnChangePacket) {
	if vl.ContextID == "" {
		vl.ContextID = talking.GetID()
	}
	if vl.Time.IsZero() {
		vl.Time = time.Now()
	}

	if talking.speechToTextTransformer != nil {
		if err := talking.speechToTextTransformer.Transform(ctx, vl); err != nil {
			talking.logger.Errorf("stt context-change update failed: %v", err)
		}
	}
	if talking.textToSpeechTransformer != nil {
		if err := talking.textToSpeechTransformer.Transform(ctx, vl); err != nil {
			talking.logger.Errorf("tts context-change update failed: %v", err)
		}
	}

	talking.OnPacket(ctx, internal_type.ConversationEventPacket{
		ContextID: vl.ContextID,
		Name:      "turn",
		Data: map[string]string{
			"type":           "change",
			"old_context_id": vl.PreviousContextID,
			"new_context_id": vl.ContextID,
			"reason":         vl.Reason,
			"source":         vl.Source,
		},
		Time: vl.Time,
	})
}

func (talking *genericRequestor) handleInterruptTTS(ctx context.Context, vl internal_type.InterruptTTSPacket) {
	if talking.textToSpeechTransformer != nil {
		// Synchronous — the TTS provider reinitializes its connection on
		// interrupt. Running inline ensures the reinit completes before the
		// critical dispatcher moves to the next packet, so any subsequent
		// InjectMessagePacket on outputCh finds TTS ready to speak.
		utils.Go(ctx, func() {
			if err := talking.textToSpeechTransformer.Transform(ctx, internal_type.InterruptionDetectedPacket{
				ContextID: vl.ContextID,
				StartAt:   vl.StartAt,
				EndAt:     vl.EndAt,
			}); err != nil {
				talking.logger.Errorf("speak: failed to send interruption to TTS: %v", err)
			}
		})
	}
}

func (talking *genericRequestor) handleInterruptLLM(ctx context.Context, vl internal_type.InterruptLLMPacket) {
	if talking.assistantExecutor != nil {
		utils.Go(ctx, func() {
			if err := talking.assistantExecutor.Execute(ctx, talking, internal_type.InterruptionDetectedPacket{ContextID: vl.ContextID}); err != nil {
				talking.logger.Errorf("LLM interruption error: %v", err)
			}
		})
	}
}

// =============================================================================
// LLM pipeline handlers
// =============================================================================

// handleExecuteLLM runs the LLM executor in a goroutine so the dispatcher
// is not blocked for the duration of the LLM response (which can be seconds).
func (talking *genericRequestor) handleExecuteLLM(ctx context.Context, vl internal_type.ExecuteLLMPacket) {
	utils.Go(ctx, func() {
		if err := talking.assistantExecutor.Execute(ctx, talking, vl.Normalized); err != nil {
			talking.OnPacket(ctx, internal_type.LLMErrorPacket{ContextID: vl.ContextID, Error: err})
		}
	})
}

func (talking *genericRequestor) handleLLMDelta(ctx context.Context, vl internal_type.LLMResponseDeltaPacket) {
	if vl.ContextID != talking.GetID() {
		talking.OnPacket(ctx, internal_type.ConversationEventPacket{
			ContextID: vl.ContextID,
			Name:      "llm",
			Data:      map[string]string{"type": "discarded", "reason": "stale_context", "current_context": talking.GetID(), "text": vl.Text},
			Time:      time.Now(),
		})
		return
	}
	if err := talking.Transition(LLMGenerating); err != nil {
		talking.logger.Errorf("messaging transition error: %v", err)
	}
	talking.OnPacket(ctx, internal_type.AggregateTextPacket{ContextID: vl.ContextID, Text: vl.Text, IsFinal: false})
}

func (talking *genericRequestor) handleLLMDone(ctx context.Context, vl internal_type.LLMResponseDonePacket) {
	if vl.ContextID != talking.GetID() {
		talking.OnPacket(ctx, internal_type.ConversationEventPacket{
			ContextID: vl.ContextID,
			Name:      "llm",
			Data:      map[string]string{"type": "discarded", "reason": "stale_context", "packet": "done", "current_context": talking.GetID(), "text": vl.Text},
			Time:      time.Now(),
		})
		return
	}
	talking.startIdleTimeoutTimer(ctx)
	if err := talking.Transition(LLMGenerated); err != nil {
		talking.logger.Errorf("messaging transition error: %v", err)
	}
	talking.OnPacket(ctx,
		internal_type.SaveMessagePacket{ContextID: vl.ContextID, MessageRole: "assistant", Text: vl.Text},
		internal_type.AssistantMessageMetricPacket{
			ContextID: vl.ContextID,
			Metrics:   []*protos.Metric{{Name: "assistant_turn", Value: type_enums.CONVERSATION_COMPLETE.String(), Description: fmt.Sprintf("LLM response completed")}},
		},
		internal_type.AggregateTextPacket{ContextID: vl.ContextID, Text: vl.Text, IsFinal: true},
	)
}

func (talking *genericRequestor) handleLLMError(ctx context.Context, vl internal_type.LLMErrorPacket) {
	_ = talking.Notify(ctx, &protos.ConversationError{
		AssistantConversationId: talking.Conversation().Id,
		Message:                 fmt.Sprintf("llm: %v", vl.Error),
	})

	talking.OnPacket(ctx, internal_type.UserMessageMetricPacket{
		ContextID: vl.ContextID,
		Metrics:   []*protos.Metric{{Name: "llm_error", Value: fmt.Sprintf("llm: %v", vl.Error), Description: "An error occurred during LLM processing"}},
	})
	talking.Transition(LLMGenerated)
}

// =============================================================================
// Static / system-injected handler
// =============================================================================

// handleInjectMessagePacket speaks a pre-written message (greeting, error, idle timeout).
//
// Both modes follow the same state lifecycle: LLMGenerating → deliver →
// LLMGenerated → start idle timer. This ensures the state machine is in
// LLMGenerated after delivery so subsequent Transition(Interrupted) calls
// (from onIdleTimeout or user interrupt) always succeed and rotate the
// context ID.
//
// Text mode delivers synchronously; audio mode flows through the text
// aggregator and TTS pipeline in a goroutine.
func (talking *genericRequestor) handleInjectMessagePacket(ctx context.Context, vl internal_type.InjectMessagePacket) {
	if err := talking.Transition(LLMGenerating); err != nil {
		talking.logger.Errorf("messaging transition error: %v", err)
	}

	// Record injected message in executor history (e.g. for LLM context).
	utils.Go(ctx, func() {
		if err := talking.assistantExecutor.Execute(ctx, talking, vl); err != nil {
			talking.logger.Errorf("assistant executor error: %v", err)
		}
	})

	// Use the CURRENT session context — not vl.ContextId(). The inject packet
	// was created before the InterruptionDetectedPacket rotated the context
	// on the same criticalCh. By the time this handler runs, GetID() returns
	// the post-interrupt context. For welcome messages (no prior interrupt),
	// GetID() returns the original context — both cases are correct.
	contextID := talking.GetID()

	// Emit as LLMResponseDelta + LLMResponseDone so the output pipeline
	// handles aggregation, TTS, state transitions (LLMGenerated), and idle
	// timer naturally — all on the output dispatcher goroutine in order.
	talking.OnPacket(ctx,
		internal_type.LLMResponseDeltaPacket{ContextID: contextID, Text: vl.Text},
		internal_type.LLMResponseDonePacket{ContextID: contextID, Text: vl.Text},
	)
}

// =============================================================================
// TTS pipeline handlers
// =============================================================================

func (talking *genericRequestor) handleSpeakText(ctx context.Context, vl internal_type.SpeakTextPacket) {
	if vl.ContextID != talking.GetID() {
		talking.OnPacket(ctx, internal_type.ConversationEventPacket{
			ContextID: vl.ContextID,
			Name:      "tts",
			Data:      map[string]string{"type": "discarded", "reason": "stale_context", "packet": "speak_text", "current_context": talking.GetID()},
			Time:      time.Now(),
		})
		return
	}

	if talking.textToSpeechTransformer != nil && talking.GetMode().Audio() {
		if vl.IsFinal {
			if err := talking.textToSpeechTransformer.Transform(ctx, internal_type.LLMResponseDonePacket{ContextID: vl.ContextID, Text: vl.Text}); err != nil {
				talking.logger.Errorf("speak: failed to send to TTS: %v", err)
			}
		} else {
			if err := talking.textToSpeechTransformer.Transform(ctx, internal_type.LLMResponseDeltaPacket{ContextID: vl.ContextID, Text: vl.Text}); err != nil {
				talking.logger.Errorf("speak: failed to send to TTS: %v", err)
			}
		}

	}
	if err := talking.Notify(ctx, &protos.ConversationAssistantMessage{
		Time:      timestamppb.Now(),
		Id:        vl.ContextID,
		Completed: vl.IsFinal,
		Message:   &protos.ConversationAssistantMessage_Text{Text: vl.Text},
	}); err != nil {
		talking.logger.Tracef(ctx, "error while outputting chunk to the user: %w", err)
	}
}

func (talking *genericRequestor) handleTTSAudio(ctx context.Context, vl internal_type.TextToSpeechAudioPacket) {
	if talking.GetMode().Audio() {
		audioInfo := internal_audio.GetAudioInfo(vl.AudioChunk, internal_audio.RAPIDA_INTERNAL_AUDIO_CONFIG)
		talking.extendIdleTimeoutTimer(time.Duration(audioInfo.DurationMs) * time.Millisecond)
	}
	if vl.ContextID != talking.GetID() {
		talking.OnPacket(ctx,
			internal_type.ConversationEventPacket{
				ContextID: vl.ContextID,
				Name:      "tts",
				Data:      map[string]string{"type": "discarded", "reason": "stale_context", "packet": "tts_audio", "current_context": talking.GetID()},
				Time:      time.Now(),
			},
			internal_type.AssistantMessageMetricPacket{
				ContextID: vl.ContextID,
				Metrics:   []*protos.Metric{{Name: "discarded_tts_chunk", Value: "true", Description: fmt.Sprintf("tts end packet discarded due to stale contextID %s", talking.GetID())}},
			})
		return
	}
	if err := talking.Notify(ctx, &protos.ConversationAssistantMessage{
		Time:      timestamppb.Now(),
		Id:        vl.ContextID,
		Message:   &protos.ConversationAssistantMessage_Audio{Audio: vl.AudioChunk},
		Completed: false,
	}); err != nil {
		talking.logger.Tracef(ctx, "error while outputting chunk to the user: %w", err)
	}
	talking.OnPacket(ctx, internal_type.RecordAssistantAudioPacket{ContextID: vl.ContextID, Audio: vl.AudioChunk})
}

func (talking *genericRequestor) handleTTSEnd(ctx context.Context, vl internal_type.TextToSpeechEndPacket) {
	if vl.ContextID != talking.GetID() {
		talking.OnPacket(ctx,
			internal_type.ConversationEventPacket{
				ContextID: vl.ContextID,
				Name:      "tts",
				Data:      map[string]string{"type": "discarded", "reason": "stale_context", "packet": "tts_end", "current_context": talking.GetID()},
				Time:      time.Now(),
			},
			internal_type.AssistantMessageMetricPacket{
				ContextID: vl.ContextID,
				Metrics:   []*protos.Metric{{Name: "discarded_tts", Value: "true", Description: fmt.Sprintf("tts end packet discarded due to stale contextID %s", talking.GetID())}},
			})
		return
	}
	if err := talking.Notify(ctx, &protos.ConversationAssistantMessage{
		Time:      timestamppb.Now(),
		Id:        vl.ContextID,
		Completed: true,
	}); err != nil {
		talking.logger.Tracef(ctx, "error while outputting chunk to the user: %w", err)
	}
}

// =============================================================================
// Persistence handlers
// =============================================================================

func (talking *genericRequestor) handleSaveMessage(ctx context.Context, vl internal_type.SaveMessagePacket) {
	if err := talking.onAddMessage(ctx, vl); err != nil {
		talking.logger.Errorf("Error in onAddMessage: %v", err)
	}
}

func (talking *genericRequestor) handleConversationMetric(ctx context.Context, vl internal_type.ConversationMetricPacket) {
	if len(vl.Metrics) > 0 {
		_ = talking.Notify(ctx, &protos.ConversationMetric{
			AssistantConversationId: talking.Conversation().Id,
			Metrics:                 vl.Metrics,
		})
		if talking.observer != nil {
			talking.observer.EmitMetric(ctx, vl.Metrics)
		}
	}
}

func (talking *genericRequestor) handleConversationMetadata(ctx context.Context, vl internal_type.ConversationMetadataPacket) {
	if len(vl.Metadata) > 0 {
		if err := talking.onAddMetadata(ctx, vl.Metadata...); err != nil {
			talking.logger.Errorf("Error in onAddMetadata: %v", err)
		}
	}
}

func (talking *genericRequestor) handleAssistantMessageMetric(ctx context.Context, vl internal_type.AssistantMessageMetricPacket) {
	if len(vl.Metrics) > 0 {
		_ = talking.Notify(ctx, &protos.ConversationMetric{
			AssistantConversationId: talking.Conversation().Id,
			Metrics:                 vl.Metrics,
		})
		if err := talking.onAddMessageMetric(ctx, "assistant", vl.ContextID, vl.Metrics); err != nil {
			talking.logger.Errorf("Error in onMessageMetric: %v", err)
		}
		if talking.observer != nil {
			talking.observer.MetricCollectors().Collect(ctx, observe.MessageMetricRecord{
				MessageID:      vl.ContextID,
				ConversationID: fmt.Sprintf("%d", talking.Conversation().Id),
				Metrics:        vl.Metrics,
				Time:           time.Now(),
			})
		}
	}
}

func (talking *genericRequestor) handleUserMessageMetric(ctx context.Context, vl internal_type.UserMessageMetricPacket) {
	if len(vl.Metrics) > 0 {
		_ = talking.Notify(ctx, &protos.ConversationMetric{
			AssistantConversationId: talking.Conversation().Id,
			Metrics:                 vl.Metrics,
		})
		if vl.ContextID == "" {
			vl.ContextID = talking.GetID()
		}
		if err := talking.onAddMessageMetric(ctx, "user", vl.ContextID, vl.Metrics); err != nil {
			talking.logger.Errorf("Error in onMessageMetric: %v", err)
		}
		if talking.observer != nil {
			talking.observer.MetricCollectors().Collect(ctx, observe.MessageMetricRecord{
				MessageID:      vl.ContextID,
				ConversationID: fmt.Sprintf("%d", talking.Conversation().Id),
				Metrics:        vl.Metrics,
				Time:           time.Now(),
			})
		}
	}
}

func (talking *genericRequestor) handleUserMessageMetadata(ctx context.Context, vl internal_type.UserMessageMetadataPacket) {
	if len(vl.Metadata) > 0 {
		_ = talking.Notify(ctx, &protos.ConversationMetadata{
			AssistantConversationId: talking.Conversation().Id,
			Metadata:                vl.Metadata,
		})
		if vl.ContextID == "" {
			vl.ContextID = talking.GetID()
		}
		if err := talking.onAddMessageMetadata(ctx, "user", vl.ContextID, vl.Metadata); err != nil {
			talking.logger.Errorf("Error in onAddMessageMetadata: %v", err)
		}
	}
}

func (talking *genericRequestor) handleAssistantMessageMetadata(ctx context.Context, vl internal_type.AssistantMessageMetadataPacket) {
	if len(vl.Metadata) > 0 {
		_ = talking.Notify(ctx, &protos.ConversationMetadata{
			AssistantConversationId: talking.Conversation().Id,
			Metadata:                vl.Metadata,
		})
		if vl.ContextID == "" {
			vl.ContextID = talking.GetID()
		}
		if err := talking.onAddMessageMetadata(ctx, "assistant", vl.ContextID, vl.Metadata); err != nil {
			talking.logger.Errorf("Error in onAddMessageMetadata: %v", err)
		}
	}
}

// =============================================================================
// Tool handlers
// =============================================================================

func (talking *genericRequestor) handleToolCall(ctx context.Context, vl internal_type.LLMToolCallPacket) {
	if talking.assistantExecutor != nil {
		if err := talking.assistantExecutor.Execute(ctx, talking, vl); err != nil {
			talking.logger.Errorf("assistant executor error: %v", err)
		}
	}
	req, _ := json.Marshal(vl)
	if err := talking.CreateToolLog(ctx, vl.ContextID, vl.ToolID, vl.Name, type_enums.RECORD_IN_PROGRESS, req); err != nil {
		talking.logger.Errorf("error logging tool call start: %v", err)
	}
	talking.OnPacket(ctx, internal_type.ConversationEventPacket{
		ContextID: vl.ContextID,
		Name:      observe.ComponentTool,
		Data:      map[string]string{observe.DataType: observe.EventToolCallStarted, "name": vl.Name, "id": vl.ToolID, "action": vl.Action.String()},
		Time:      time.Now(),
	})
	talking.Notify(ctx, &protos.ConversationToolCall{
		Id:     vl.ContextID,
		ToolId: vl.ToolID,
		Name:   vl.Name,
		Action: vl.Action,
		Args:   vl.Arguments,
		Time:   timestamppb.Now(),
	})

}

func (talking *genericRequestor) handleToolResult(ctx context.Context, vl internal_type.LLMToolResultPacket) {
	if talking.assistantExecutor != nil {
		if err := talking.assistantExecutor.Execute(ctx, talking, vl); err != nil {
			talking.logger.Errorf("tool result processing failed: %v", err)
		}
	}
	res, _ := json.Marshal(vl)
	if err := talking.UpdateToolLog(ctx, vl.ToolID, type_enums.RECORD_COMPLETE, res); err != nil {
		talking.logger.Errorf("error logging tool call result: %v", err)
	}

	talking.OnPacket(ctx, internal_type.ConversationEventPacket{
		ContextID: vl.ContextID,
		Name:      observe.ComponentTool,
		Data:      map[string]string{observe.DataType: observe.EventToolCallCompleted, "name": vl.Name, "id": vl.ToolID},
		Time:      time.Now(),
	})

}

// =============================================================================
// Observability handler
// =============================================================================

// handleConversationEvent forwards a ConversationEventPacket to the debugger
// via the gRPC stream. If the packet's ContextID is empty (e.g. emitted by an
// STT callback that doesn't hold the session context) the current interaction
// context ID from r.GetID() is used.
func (talking *genericRequestor) handleConversationEvent(ctx context.Context, vl internal_type.ConversationEventPacket) {
	contextID := vl.ContextID
	if contextID == "" {
		contextID = talking.GetID()
	}
	if vl.Time.IsZero() {
		vl.Time = time.Now()
	}
	_ = talking.Notify(ctx, &protos.ConversationEvent{
		Id:   contextID,
		Name: vl.Name,
		Data: vl.Data,
		Time: timestamppb.New(vl.Time),
	})
	if talking.observer != nil {
		talking.observer.EventCollectors().Collect(ctx, observe.EventRecord{
			MessageID: contextID,
			Name:      vl.Name,
			Data:      vl.Data,
			Time:      vl.Time,
		})
	}
}
