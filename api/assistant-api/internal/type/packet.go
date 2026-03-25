// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.
package internal_type

import (
	"fmt"
	"time"

	"github.com/rapidaai/protos"
)

// Packet represents a generic request packet handled by the adapter layer.
// Concrete packet types (e.g., FlushPacket, InterruptionPacket) are used to
// signal specific control actions within a given context.
type Packet interface {
	ContextId() string
}

// Wrapper for message packet
type MessagePacket interface {
	Packet
	Role() string
	Content() string
}

type AudioPacket interface {
	Packet
	Content() []byte
}

// InterruptionPacket represents a request to interrupt ongoing processing
// within a specific context.

// =============================================================================
// LLM Packets
// =============================================================================

type InterruptionSource string

const (
	InterruptionSourceWord InterruptionSource = "word"
	InterruptionSourceVad  InterruptionSource = "vad"
)

type InterruptionPacket struct {
	// ContextID identifies the context to be interrupted.
	ContextID string

	// Source indicates the origin of the interruption.
	Source InterruptionSource

	// start of interruption
	StartAt float64

	// end of interruption
	EndAt float64
}

// ContextId returns the identifier of the context associated with this interruption request.
func (f InterruptionPacket) ContextId() string {
	return f.ContextID
}

// =============================================================================
// LLM Packets
// =============================================================================

// ConversationMetricPacket represents a request to send metrics within a specific context.
type ConversationMetricPacket struct {

	// ContextID identifies the context to be flushed.
	ContextID uint64

	// Metrics holds the list of metrics to be sent within the specified context.
	Metrics []*protos.Metric
}

func (f ConversationMetricPacket) ContextId() string {
	return fmt.Sprintf("%d", f.ContextID)
}

func (f ConversationMetricPacket) ConversationID() uint64 {
	return f.ContextID
}

type ConversationMetadataPacket struct {

	// ContextID identifies the context to be flushed.
	ContextID uint64

	// Metadata holds the list of metadata to be sent within the specified context.
	Metadata []*protos.Metadata
}

func (f ConversationMetadataPacket) ContextId() string {
	return fmt.Sprintf("%d", f.ContextID)
}

func (f ConversationMetadataPacket) ConversationID() uint64 {
	return f.ContextID
}

// ConversationMetricPacket represents a request to send metrics within a specific context.
type MessageMetricPacket struct {

	// ContextID identifies the context to be flushed.
	ContextID string

	// Metrics holds the list of metrics to be sent within the specified context.
	Metrics []*protos.Metric
}

func (f MessageMetricPacket) ContextId() string {
	return f.ContextID
}

type MessageMetadataPacket struct {

	// ContextID identifies the context to be flushed.
	ContextID string

	// Metadata holds the list of metadata to be sent within the specified context.
	Metadata []*protos.Metadata
}

func (f MessageMetadataPacket) ContextId() string {
	return f.ContextID
}

// =============================================================================
// Directive Packets
// =============================================================================

type DirectivePacket struct {
	// ContextID identifies the context to be flushed.
	ContextID string

	// Directive
	Directive protos.ConversationDirective_DirectiveType

	// arguments for directive
	Arguments map[string]interface{}
}

func (f DirectivePacket) ContextId() string {
	return f.ContextID
}

// =============================================================================
// LLM Packets
// =============================================================================

type LLMPacket interface {
	Packet
	ContextId() string
}

type LLMErrorPacket struct {
	// contextID identifies the context to be flushed.
	ContextID string

	// error
	Error error

	//

}

func (f LLMErrorPacket) ContextId() string {
	return f.ContextID
}

// LLMResponseDeltaPacket represents a streaming text delta from the LLM.
// This packet is emitted during streaming responses, containing partial text chunks.
type LLMResponseDeltaPacket struct {
	// ContextID identifies the context for this response.
	ContextID string

	// Text contains the partial text content of this delta.
	Text string
}

func (f LLMResponseDeltaPacket) ContextId() string {
	return f.ContextID
}

// LLMResponseDonePacket signals the completion of an LLM response stream.
// This packet is emitted when the LLM has finished generating its response.
type LLMResponseDonePacket struct {
	// ContextID identifies the context for this response.
	ContextID string

	// Text contains the final aggregated text (optional, may be empty for streaming).
	Text string
}

func (f LLMResponseDonePacket) Content() string {
	return f.Text
}

func (f LLMResponseDonePacket) Role() string {
	return "assistant"
}

func (f LLMResponseDonePacket) ContextId() string {
	return f.ContextID
}

// =============================================================================
// LLM Tool Call Packets
// =============================================================================

type LLMToolPacket interface {
	ToolId() string
}

type LLMToolCallPacket struct {
	// id of tool which user has configured
	ToolID string

	// name of tool which user has configured
	Name string

	// contextID identifies the context to be flushed.
	ContextID string

	// arguments for tool call
	Arguments map[string]interface{}
}

func (f LLMToolCallPacket) ContextId() string {
	return f.ContextID
}

func (f LLMToolCallPacket) ToolId() string {
	return f.ToolID
}

type LLMToolResultPacket struct {
	// id of tool which user has configured
	ToolID string

	// name of tool which user has configured
	Name string

	// contextID identifies the context to be flushed.
	ContextID string

	// time taken for tool execution in nanoseconds
	TimeTaken int64

	// result for tool call
	Result map[string]interface{}
}

func (f LLMToolResultPacket) ToolId() string {
	return f.ToolID
}

func (f LLMToolResultPacket) ContextId() string {
	return f.ContextID
}

// =============================================================================
// LLM Packets end
// =============================================================================

type StaticPacket struct {
	// contextID identifies the context to be flushed.
	ContextID string

	// message
	Text string
}

func (f StaticPacket) ContextId() string {
	return f.ContextID
}

func (f StaticPacket) Content() string {
	return f.Text
}

func (f StaticPacket) Role() string {
	return "rapida"
}

// =============================================================================
// LLM Packets end
// =============================================================================

type TextToSpeechAudioPacket struct {

	// contextID identifies the context to be flushed.
	ContextID string

	// audio chunk
	AudioChunk []byte
}

func (f TextToSpeechAudioPacket) ContextId() string {
	return f.ContextID
}

type TextToSpeechEndPacket struct {
	// contextID identifies the context to be flushed.
	ContextID string
}

func (f TextToSpeechEndPacket) ContextId() string {
	return f.ContextID
}

// =============================================================================
// User Packet
// =============================================================================

type UserTextPacket struct {
	// contextID identifies the context to be flushed.
	ContextID string

	// text
	Text string

	// Language detected by STT for this turn (may be empty for text-mode input).
	Language string
}

func (f UserTextPacket) ContextId() string {
	return f.ContextID
}

func (f UserTextPacket) Content() string {
	return f.Text
}

func (f UserTextPacket) Role() string {
	return "user"
}

type UserAudioPacket struct {
	// contextID identifies the context to be flushed.
	ContextID string

	Audio []byte

	NoiseReduced bool
}

func (f UserAudioPacket) ContextId() string {
	return f.ContextID
}

func (f UserAudioPacket) Content() []byte {
	return f.Audio
}

func (f UserAudioPacket) Role() string {
	return "user"
}

// =============================================================================
// End of speech Packet
// =============================================================================

type EndOfSpeechPacket struct {
	// contextID identifies the context to be flushed.
	ContextID string

	Speech string

	// Language detected by the STT provider for this turn.
	Language string
}

func (f EndOfSpeechPacket) ContextId() string {
	return f.ContextID
}

type InterimEndOfSpeechPacket struct {
	// contextID identifies the context being updated.
	ContextID string

	Speech string
}

func (p InterimEndOfSpeechPacket) ContextId() string {
	return p.ContextID
}

type SpeechToTextPacket struct {
	ContextID string

	// script
	Script string

	// confidence
	Confidence float64

	// language
	Language string

	// interim
	Interim bool
}

func (f SpeechToTextPacket) ContextId() string {
	return f.ContextID
}

// =============================================================================
// Pre-processing Packets
// =============================================================================

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

// =============================================================================
// LLM Execution Packet
// =============================================================================

// ExecuteLLMPacket triggers the LLM pipeline with the user's final transcript.
type ExecuteLLMPacket struct {
	ContextID string

	Input string

	// Language detected by the STT provider for this turn.
	Language string
}

func (f ExecuteLLMPacket) ContextId() string { return f.ContextID }

// =============================================================================
// TTS Packet
// =============================================================================

// SpeakTextPacket routes text into the TTS pipeline or directly to the client.
// IsFinal=true signals a flush (end of generation); IsFinal=false is a streaming delta.
type SpeakTextPacket struct {
	ContextID string
	Text      string
	IsFinal   bool
}

func (f SpeakTextPacket) ContextId() string { return f.ContextID }

// =============================================================================
// Interrupt Packets
// =============================================================================

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

// =============================================================================
// Recording Packets
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
// Persistence Packet
// =============================================================================

// SaveMessagePacket persists a conversation message to the database and appends
// it to the in-memory history. It implements MessagePacket so it can be passed
// directly to onCreateMessage.
type SaveMessagePacket struct {
	ContextID   string
	MessageRole string
	Text        string

	// Language detected by the STT provider for this turn (user messages only).
	Language string
}

func (f SaveMessagePacket) ContextId() string { return f.ContextID }
func (f SaveMessagePacket) Role() string      { return f.MessageRole }
func (f SaveMessagePacket) Content() string   { return f.Text }

//

// KnowledgeRetrieveOption contains options for knowledge retrieval operations
type KnowledgeRetrieveOption struct {
	EmbeddingProviderCredential *protos.VaultCredential
	RetrievalMethod             string
	TopK                        uint32
	ScoreThreshold              float32
}

type KnowledgeContextResult struct {
	ID         string                 `json:"id"`
	DocumentID string                 `json:"document_id"`
	Metadata   map[string]interface{} `json:"metadata"`
	Content    string                 `json:"content"`
	Score      float64                `json:"score"`
}

// =============================================================================
// Observability Packets
// =============================================================================

// ConversationEventPacket carries a named pipeline event for the debugger.
// Each component emits these alongside its existing packets; they flow through
// lowCh so they never compete with STT/LLM/TTS audio.
type ConversationEventPacket struct {
	// ContextID identifies the interaction turn. May be empty when emitted by
	// components that don't hold the session context (e.g. STT callbacks);
	// handleConversationEvent fills it from r.GetID() in that case.
	ContextID string

	// EventName is the component name: "stt", "tts", "llm", "vad", "eos",
	// "knowledge", "session", "behavior", "denoise", "audio", "tool", etc.
	Name string

	// Data carries event-specific key/value pairs. Always includes "type".
	Data map[string]string

	// OccurredAt is the wall-clock time the event was raised.
	Time time.Time
}

func (f ConversationEventPacket) ContextId() string { return f.ContextID }
