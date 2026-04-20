// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.
package internal_type

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/rapidaai/pkg/types"
	"github.com/rapidaai/protos"
)

// =============================================================================
// Packet Interfaces
// =============================================================================

// Packet represents a generic request packet handled by the adapter layer.
// Concrete packet types signal specific actions or events within a given context.
//
// Naming convention:
//   - Commands (trigger an action): verb-first — ExecuteLLM, DenoiseAudio, InterruptTTS, RecordUserAudio, SaveMessage, SpeakText
//   - Events  (something happened): past-tense/noun — LLMResponseDelta, DenoisedAudio, SpeechToText, EndOfSpeech, TextToSpeechAudio
type Packet interface {
	ContextId() string
}

// MessagePacket wraps a Packet with role and text content.
type MessagePacket interface {
	Packet
	Role() string
	Content() string
}

// AudioPacket wraps a Packet with raw audio bytes.
type AudioPacket interface {
	Packet
	Content() []byte
}

// LLMPacket is a marker interface for LLM pipeline packets.
type LLMPacket interface {
	Packet
	ContextId() string
}

// LLMToolPacket is a marker interface for LLM tool-related packets.
type LLMToolPacket interface {
	ToolId() string
}

// =============================================================================
// Input Pipeline — user -> denoise -> VAD -> STT -> EOS -> normalize
// =============================================================================

// UserTextReceivedPacket carries text input from the user (e.g. via WebSocket/HTTP).
type UserTextReceivedPacket struct {
	ContextID string
	Text      string

	// Language detected by STT for this turn (may be empty for text-mode input).
	Language string
}

func (f UserTextReceivedPacket) ContextId() string { return f.ContextID }
func (f UserTextReceivedPacket) Content() string   { return f.Text }
func (f UserTextReceivedPacket) Role() string      { return "user" }

// UserAudioReceivedPacket carries raw audio input from the user (e.g. via WebRTC).
type UserAudioReceivedPacket struct {
	ContextID    string
	Audio        []byte
	NoiseReduced bool
}

func (f UserAudioReceivedPacket) ContextId() string { return f.ContextID }
func (f UserAudioReceivedPacket) Content() []byte   { return f.Audio }
func (f UserAudioReceivedPacket) Role() string      { return "user" }

// DenoiseAudioPacket carries raw user audio to be denoised before entering the pipeline.
type DenoiseAudioPacket struct {
	ContextID string
	Audio     []byte
}

func (f DenoiseAudioPacket) ContextId() string { return f.ContextID }

// DenoisedAudioPacket carries the result of the denoiser stage.
// The denoiser pushes this via onPacket instead of returning bytes to the caller.
// On error the denoiser falls back to the original audio with NoiseReduced=false.
type DenoisedAudioPacket struct {
	ContextID    string
	Audio        []byte
	Confidence   float64
	NoiseReduced bool
}

func (f DenoisedAudioPacket) ContextId() string { return f.ContextID }

// VadAudioPacket carries a processed audio chunk to submit to the VAD processor.
type VadAudioPacket struct {
	ContextID string
	Audio     []byte
}

func (f VadAudioPacket) ContextId() string { return f.ContextID }

// VadSpeechActivityPacket is a lightweight heartbeat emitted by the VAD on every
// audio chunk where the user is actively speaking. The EOS detector uses it to
// keep extending the silence timer during sustained speech.
type VadSpeechActivityPacket struct{}

func (f VadSpeechActivityPacket) ContextId() string { return "" }

// SpeechToTextPacket carries a transcript result from the STT provider.
type SpeechToTextPacket struct {
	ContextID  string
	Script     string
	Confidence float64
	Language   string
	Interim    bool
}

func (f SpeechToTextPacket) ContextId() string { return f.ContextID }

// EndOfSpeechPacket signals that the EOS detector determined the user's turn is complete.
type EndOfSpeechPacket struct {
	ContextID string
	Speech    string
	Speechs   []SpeechToTextPacket // accumulated transcript chunks
}

func (f EndOfSpeechPacket) ContextId() string { return f.ContextID }

// InterimEndOfSpeechPacket carries a partial EOS result (in-progress transcript).
type InterimEndOfSpeechPacket struct {
	ContextID string
	Speech    string
}

func (p InterimEndOfSpeechPacket) ContextId() string { return p.ContextID }

// NormalizedUserTextPacket carries the final normalized user text after language detection.
type NormalizedUserTextPacket struct {
	ContextID string
	Text      string
	Language  types.Language
}

func (f NormalizedUserTextPacket) ContextId() string { return f.ContextID }

// NormalizeInputPacket triggers the input normalizer with the finalized speech.
// Isolates the normalization step so it can be swapped or skipped without
// modifying the EndOfSpeech handler.
type NormalizeInputPacket struct {
	ContextID string
	Speech    string
	Speechs   []SpeechToTextPacket
}

func (f NormalizeInputPacket) ContextId() string { return f.ContextID }

// =============================================================================
// Control — interrupts, directives, injected messages
// =============================================================================

type InterruptionSource string

const (
	InterruptionSourceWord InterruptionSource = "word"
	InterruptionSourceVad  InterruptionSource = "vad"
)

// InterruptionDetectedPacket signals that an interruption was detected (by VAD or word-level STT).
// Dispatch handles this event by emitting InterruptTTSPacket and InterruptLLMPacket commands.
type InterruptionDetectedPacket struct {
	ContextID string
	Source    InterruptionSource
	StartAt   float64
	EndAt     float64
}

func (f InterruptionDetectedPacket) ContextId() string { return f.ContextID }

// InterruptTTSPacket signals the TTS transformer to stop current playback.
type InterruptTTSPacket struct {
	ContextID string
	StartAt   float64
	EndAt     float64
}

func (f InterruptTTSPacket) ContextId() string { return f.ContextID }

// InterruptLLMPacket signals the LLM executor to cancel current generation.
type InterruptLLMPacket struct {
	ContextID string
}

func (f InterruptLLMPacket) ContextId() string { return f.ContextID }

// TurnChangePacket notifies components that active context changed to a new turn.
type TurnChangePacket struct {
	ContextID         string
	PreviousContextID string
	Reason            string
	Source            string
	Time              time.Time
}

func (f TurnChangePacket) ContextId() string { return f.ContextID }

// InjectMessagePacket injects a pre-written message (greeting, error, idle timeout) into the pipeline.
type InjectMessagePacket struct {
	ContextID string
	Text      string
}

func (f InjectMessagePacket) ContextId() string { return f.ContextID }
func (f InjectMessagePacket) Content() string   { return f.Text }
func (f InjectMessagePacket) Role() string      { return "rapida" }

// =============================================================================
// LLM Pipeline — execute -> delta -> done -> error -> tools
// =============================================================================

// ExecuteLLMPacket triggers the LLM pipeline with the user's final transcript.
type ExecuteLLMPacket struct {
	ContextID  string
	Input      string
	Normalized NormalizedUserTextPacket
}

func (f ExecuteLLMPacket) ContextId() string { return f.ContextID }

// LLMResponseDeltaPacket represents a streaming text delta from the LLM.
type LLMResponseDeltaPacket struct {
	ContextID string
	Text      string
}

func (f LLMResponseDeltaPacket) ContextId() string { return f.ContextID }

// LLMResponseDonePacket signals the completion of an LLM response stream.
type LLMResponseDonePacket struct {
	ContextID string
	Text      string
}

func (f LLMResponseDonePacket) Content() string   { return f.Text }
func (f LLMResponseDonePacket) Role() string      { return "assistant" }
func (f LLMResponseDonePacket) ContextId() string { return f.ContextID }

// LLMErrorPacket signals that the LLM encountered an error during generation.
type LLMErrorPacket struct {
	ContextID string
	Error     error
}

func (f LLMErrorPacket) ContextId() string { return f.ContextID }

// LLMToolCallPacket signals that a tool was invoked.
// Action determines whether the client/channel needs to act (e.g. end call, transfer).
type LLMToolCallPacket struct {
	ToolID    string
	Name      string
	ContextID string
	Action    protos.ToolCallAction
	Arguments map[string]string
}

func (f LLMToolCallPacket) ContextId() string { return f.ContextID }
func (f LLMToolCallPacket) ToolId() string    { return f.ToolID }

func (f LLMToolCallPacket) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]interface{}{
		"tool_id":   f.ToolID,
		"name":      f.Name,
		"context_id": f.ContextID,
		"action":    f.Action.String(),
		"arguments": f.Arguments,
	})
}

func (f *LLMToolCallPacket) UnmarshalJSON(data []byte) error {
	var raw struct {
		ToolID    string            `json:"tool_id"`
		Name      string            `json:"name"`
		ContextID string            `json:"context_id"`
		Action    string            `json:"action"`
		Arguments map[string]string `json:"arguments"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	f.ToolID = raw.ToolID
	f.Name = raw.Name
	f.ContextID = raw.ContextID
	f.Action = protos.ToolCallAction(protos.ToolCallAction_value[raw.Action])
	f.Arguments = raw.Arguments
	return nil
}

// LLMToolResultPacket carries the result of a tool execution.
// Arrives from server-side tools (immediate) or from client/channel (directive).
type LLMToolResultPacket struct {
	ToolID    string
	Name      string
	ContextID string
	Action    protos.ToolCallAction
	Result    map[string]string
}

func (f LLMToolResultPacket) ToolId() string    { return f.ToolID }
func (f LLMToolResultPacket) ContextId() string { return f.ContextID }

func (f LLMToolResultPacket) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]interface{}{
		"tool_id":    f.ToolID,
		"name":       f.Name,
		"context_id": f.ContextID,
		"action":     f.Action.String(),
		"result":     f.Result,
	})
}

func (f *LLMToolResultPacket) UnmarshalJSON(data []byte) error {
	var raw struct {
		ToolID    string            `json:"tool_id"`
		Name      string            `json:"name"`
		ContextID string            `json:"context_id"`
		Action    string            `json:"action"`
		Result    map[string]string `json:"result"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	f.ToolID = raw.ToolID
	f.Name = raw.Name
	f.ContextID = raw.ContextID
	f.Action = protos.ToolCallAction(protos.ToolCallAction_value[raw.Action])
	f.Result = raw.Result
	return nil
}

// =============================================================================
// Output Pipeline — aggregate -> speak -> TTS audio -> TTS end
// =============================================================================

// AggregateTextPacket triggers the text aggregator with validated LLM output.
// The aggregator batches deltas into sentence-sized chunks before emitting
// SpeakTextPacket. IsFinal=true signals end of generation.
type AggregateTextPacket struct {
	ContextID string
	Text      string
	IsFinal   bool
}

func (f AggregateTextPacket) ContextId() string { return f.ContextID }

// SpeakTextPacket routes text into the TTS pipeline or directly to the client.
// IsFinal=true signals a flush (end of generation); IsFinal=false is a streaming delta.
type SpeakTextPacket struct {
	ContextID string
	Text      string
	IsFinal   bool
}

func (f SpeakTextPacket) ContextId() string { return f.ContextID }

// TextToSpeechAudioPacket carries a TTS audio chunk produced by the TTS provider.
type TextToSpeechAudioPacket struct {
	ContextID  string
	AudioChunk []byte
}

func (f TextToSpeechAudioPacket) ContextId() string { return f.ContextID }

// TextToSpeechEndPacket signals that TTS has finished producing audio.
type TextToSpeechEndPacket struct {
	ContextID string
}

func (f TextToSpeechEndPacket) ContextId() string { return f.ContextID }

// =============================================================================
// Recording
// =============================================================================

// RecordUserAudioPacket carries a user audio chunk to be written to the recorder.
type RecordUserAudioPacket struct {
	ContextID string
	Audio     []byte
}

func (f RecordUserAudioPacket) ContextId() string { return f.ContextID }

// RecordAssistantAudioPacket carries an assistant audio chunk to the recorder.
// When Truncate is true, the recorder trims buffered assistant audio at the current
// wall-clock position, mirroring the streamer's ClearOutputBuffer on interruption.
type RecordAssistantAudioPacket struct {
	ContextID string
	Audio     []byte
	Truncate  bool
}

func (f RecordAssistantAudioPacket) ContextId() string { return f.ContextID }

// =============================================================================
// Persistence
// =============================================================================

// SaveMessagePacket persists a conversation message to the database and appends
// it to the in-memory history. It implements MessagePacket so it can be passed
// directly to onCreateMessage.
type SaveMessagePacket struct {
	ContextID   string
	MessageRole string
	Text        string
}

func (f SaveMessagePacket) ContextId() string { return f.ContextID }
func (f SaveMessagePacket) Role() string      { return f.MessageRole }
func (f SaveMessagePacket) Content() string   { return f.Text }

// =============================================================================
// Metrics & Metadata
// =============================================================================

// ConversationMetricPacket carries conversation-level metrics.
type ConversationMetricPacket struct {
	ContextID uint64
	Metrics   []*protos.Metric
}

func (f ConversationMetricPacket) ContextId() string      { return fmt.Sprintf("%d", f.ContextID) }
func (f ConversationMetricPacket) ConversationID() uint64 { return f.ContextID }

// ConversationMetadataPacket carries conversation-level metadata.
type ConversationMetadataPacket struct {
	ContextID uint64
	Metadata  []*protos.Metadata
}

func (f ConversationMetadataPacket) ContextId() string      { return fmt.Sprintf("%d", f.ContextID) }
func (f ConversationMetadataPacket) ConversationID() uint64 { return f.ContextID }

// UserMessageMetricPacket carries metrics for a user message turn.
type UserMessageMetricPacket struct {
	ContextID string
	Metrics   []*protos.Metric
}

func (f UserMessageMetricPacket) ContextId() string { return f.ContextID }

// AssistantMessageMetricPacket carries metrics for an assistant message turn.
type AssistantMessageMetricPacket struct {
	ContextID string
	Metrics   []*protos.Metric
}

func (f AssistantMessageMetricPacket) ContextId() string { return f.ContextID }

// AssistantMessageMetadataPacket carries metadata for an assistant message turn.
type AssistantMessageMetadataPacket struct {
	ContextID string
	Metadata  []*protos.Metadata
}

func (f AssistantMessageMetadataPacket) ContextId() string { return f.ContextID }

// UserMessageMetadataPacket carries metadata for a user message turn.
type UserMessageMetadataPacket struct {
	ContextID string
	Metadata  []*protos.Metadata
}

func (f UserMessageMetadataPacket) ContextId() string { return f.ContextID }

// =============================================================================
// Observability
// =============================================================================

// ConversationEventPacket carries a named pipeline event for the debugger.
// Each component emits these alongside its existing packets; they flow through
// lowCh so they never compete with STT/LLM/TTS audio.
type ConversationEventPacket struct {
	// ContextID identifies the interaction turn. May be empty when emitted by
	// components that don't hold the session context (e.g. STT callbacks);
	// handleConversationEvent fills it from r.GetID() in that case.
	ContextID string

	// Name is the component name: "stt", "tts", "llm", "vad", "eos",
	// "knowledge", "session", "behavior", "denoise", "audio", "tool", etc.
	Name string

	// Data carries event-specific key/value pairs. Always includes "type".
	Data map[string]string

	// Time is the wall-clock time the event was raised.
	Time time.Time
}

func (f ConversationEventPacket) ContextId() string { return f.ContextID }

// =============================================================================
// Non-packet Support Types
// =============================================================================

// KnowledgeRetrieveOption contains options for knowledge retrieval operations.
type KnowledgeRetrieveOption struct {
	EmbeddingProviderCredential *protos.VaultCredential
	RetrievalMethod             string
	TopK                        uint32
	ScoreThreshold              float32
}

// KnowledgeContextResult holds a single knowledge retrieval result.
type KnowledgeContextResult struct {
	ID         string                 `json:"id"`
	DocumentID string                 `json:"document_id"`
	Metadata   map[string]interface{} `json:"metadata"`
	Content    string                 `json:"content"`
	Score      float64                `json:"score"`
}
