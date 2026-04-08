// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

package observe

import "github.com/rapidaai/protos"

// =============================================================================
// Event Components — the "who" that emitted the event
// =============================================================================

const (
	// ComponentSession is the conversation session lifecycle.
	ComponentSession = "session"

	// ComponentSIP is the SIP signaling layer.
	ComponentSIP = "sip"

	// ComponentTelephony is the telephony provider layer (Twilio, Asterisk, etc.)
	ComponentTelephony = "telephony"

	// ComponentWebRTC is the WebRTC peer connection layer.
	ComponentWebRTC = "webrtc"

	// ComponentSTT is the speech-to-text transformer.
	ComponentSTT = "stt"

	// ComponentTTS is the text-to-speech transformer.
	ComponentTTS = "tts"

	// ComponentLLM is the language model executor.
	ComponentLLM = "llm"

	// ComponentVAD is the voice activity detector.
	ComponentVAD = "vad"

	// ComponentEOS is the end-of-speech detector.
	ComponentEOS = "eos"

	// ComponentDenoise is the audio denoiser.
	ComponentDenoise = "denoise"

	// ComponentTool is the tool/function calling layer.
	ComponentTool = "tool"

	// ComponentKnowledge is the knowledge base retrieval layer.
	ComponentKnowledge = "knowledge"

	// ComponentRecording is the audio recording layer.
	ComponentRecording = "recording"
)

// =============================================================================
// Event Types — the "what" that happened
// =============================================================================

const (
	EventConnected           = "connected"
	EventConnectFailed       = "connect_failed"
	EventDisconnected        = "disconnected"
	EventDisconnectRequested = "disconnect_requested"
	EventCompleted           = "completed"
	EventModeSwitch          = "mode_switch"
	EventResumed             = "resumed"
	EventSessionResolved     = "session_resolved"
	EventSessionResolveFailed = "session_resolve_failed"
	EventStreamerCreated     = "streamer_created"
	EventStreamerFailed      = "streamer_failed"
	EventTalkerCreated       = "talker_created"
	EventTalkerFailed        = "talker_failed"
	EventTalkStarted         = "talk_started"
	EventHooksBegin          = "hooks_begin"
	EventHooksEnd            = "hooks_end"

	EventCallReceived       = "call_received"
	EventCallAnswered       = "call_answered"
	EventCallStarted        = "call_started"
	EventCallEnded          = "call_ended"
	EventCallFailed         = "call_failed"
	EventCallCompleted      = "call_completed"
	EventOutboundRequested  = "outbound_requested"
	EventOutboundDialed     = "outbound_dialed"
	EventOutboundDispatched = "outbound_dispatched"
	EventOutboundDispatchFailed = "outbound_dispatch_failed"
	EventProviderAnswered   = "provider_answered"
	EventSessionConnected   = "session_connected"
	EventAssistantLoaded    = "assistant_loaded"
	EventConversationCreated = "conversation_created"
	EventContextSaved       = "context_saved"

	// --- SIP-specific ---
	EventInviteReceived     = "invite_received"
	EventRouteResolved      = "route_resolved"
	EventAuthenticated      = "authenticated"
	EventByeReceived        = "bye_received"
	EventCancelReceived     = "cancel_received"
	EventHold               = "hold"
	EventResume             = "resume"
	EventReInvite           = "reinvite"
	EventTransferRequested  = "transfer_requested"
	EventRegisterActive     = "register_active"
	EventRegisterFailed     = "register_failed"
	EventDTMF               = "dtmf"

	// --- WebRTC-specific ---
	EventICEConnected       = "ice_connected"
	EventICEFailed          = "ice_failed"
	EventPeerConnected      = "peer_connected"
	EventPeerDisconnected   = "peer_disconnected"

	// --- Recording ---
	EventRecordingStarted   = "recording_started"
	EventRecordingStopped   = "recording_stopped"

	// --- Errors ---
	EventError              = "error"
)

// =============================================================================
// Metric Names — standardized across all channels
// =============================================================================

const (
	// --- Call duration ---
	MetricCallDurationMs     = "call.duration_ms"
	MetricSetupDurationMs    = "call.setup_duration_ms"
	MetricRingDurationMs     = "call.ring_duration_ms"

	// --- Call status ---
	MetricCallStatus         = "call.status"
	MetricCallEndReason      = "call.end_reason"
	MetricCallFailed         = "call.failed"

	// --- SIP ---
	MetricSIPRegisterFailure = "sip.register_failure"

	// --- RTP ---
	MetricRTPPacketsSent     = "rtp.packets_sent"
	MetricRTPPacketsReceived = "rtp.packets_received"
	MetricRTPBytesSent       = "rtp.bytes_sent"
	MetricRTPBytesReceived   = "rtp.bytes_received"

	// --- WebRTC ---
	MetricICELatencyMs       = "webrtc.ice_latency_ms"

	// --- Telephony ---
	MetricTelephonyStatus    = "telephony.status"
)

// =============================================================================
// Data Keys — standardized keys for event Data maps
// =============================================================================

const (
	DataType       = "type"
	DataProvider   = "provider"
	DataDirection  = "direction"
	DataReason     = "reason"
	DataError      = "error"
	DataStage      = "stage"
	DataDID        = "did"
	DataCaller     = "caller"
	DataCallee     = "callee"
	DataContextID  = "context_id"
	DataCodec      = "codec"
	DataMode       = "mode"
	DataFrom       = "from"
	DataTo         = "to"
	DataDuration   = "duration_ms"
	DataMessages   = "messages"
	DataDigit      = "digit"
)

// CallStatusMetric returns a CONVERSATION_STATUS metric for call lifecycle tracking.
func CallStatusMetric(status, reason string) []*protos.Metric {
	return []*protos.Metric{
		{Name: "status", Value: status, Description: reason},
	}
}
