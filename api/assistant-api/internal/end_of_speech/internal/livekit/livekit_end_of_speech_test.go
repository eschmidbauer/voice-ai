// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.
package internal_livekit

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- tokenizer tests ---

func TestBuildByteEncoder(t *testing.T) {
	tok := &tokenizer{}
	tok.buildByteEncoder()

	// Printable ASCII maps to itself
	assert.Equal(t, "A", tok.byteToStr['A'])
	assert.Equal(t, "z", tok.byteToStr['z'])
	assert.Equal(t, "!", tok.byteToStr['!'])

	// Space (0x20) is not in printable range, should map to extended unicode
	assert.NotEqual(t, " ", tok.byteToStr[' '])
	assert.True(t, len(tok.byteToStr[' ']) > 0)
}

func TestIsGPT2PrintableByte(t *testing.T) {
	assert.True(t, isGPT2PrintableByte('A'))
	assert.True(t, isGPT2PrintableByte('~'))
	assert.True(t, isGPT2PrintableByte('!'))
	assert.True(t, isGPT2PrintableByte(0xa1))
	assert.True(t, isGPT2PrintableByte(0xae))
	assert.True(t, isGPT2PrintableByte(0xff))
	assert.False(t, isGPT2PrintableByte(' '))
	assert.False(t, isGPT2PrintableByte('\t'))
	assert.False(t, isGPT2PrintableByte('\n'))
	assert.False(t, isGPT2PrintableByte(0x00))
	assert.False(t, isGPT2PrintableByte(0xad)) // between 0xac and 0xae
}

func TestApplyMerge(t *testing.T) {
	tok := &tokenizer{}

	symbols := []string{"a", "b", "c", "d"}
	result := tok.applyMerge(symbols, mergePair{a: "b", b: "c"})
	assert.Equal(t, []string{"a", "bc", "d"}, result)

	// No match
	result = tok.applyMerge(symbols, mergePair{a: "x", b: "y"})
	assert.Equal(t, []string{"a", "b", "c", "d"}, result)

	// Multiple matches
	symbols = []string{"a", "b", "a", "b"}
	result = tok.applyMerge(symbols, mergePair{a: "a", b: "b"})
	assert.Equal(t, []string{"ab", "ab"}, result)
}

func TestSplitOnSpecialTokens(t *testing.T) {
	tok := &tokenizer{
		special: map[string]int{
			"<|im_start|>": 49153,
			"<|im_end|>":   49154,
		},
	}

	segments := tok.splitOnSpecialTokens("<|im_start|>user\nhello<|im_end|>")
	assert.Equal(t, []string{"<|im_start|>", "user\nhello", "<|im_end|>"}, segments)

	// No special tokens
	segments = tok.splitOnSpecialTokens("plain text")
	assert.Equal(t, []string{"plain text"}, segments)

	// Only special tokens
	segments = tok.splitOnSpecialTokens("<|im_start|><|im_end|>")
	assert.Equal(t, []string{"<|im_start|>", "<|im_end|>"}, segments)
}

// --- chat_template tests ---

func TestFormatChatTemplateFromHistory_Empty(t *testing.T) {
	result := formatChatTemplateFromHistory(nil, "", 5)
	assert.Equal(t, "", result)
}

func TestFormatChatTemplateFromHistory_CurrentOnly(t *testing.T) {
	result := formatChatTemplateFromHistory(nil, "hello", 5)
	assert.Equal(t, "<|im_start|>user\nhello", result)
}

func TestFormatChatTemplateFromHistory_WithHistory(t *testing.T) {
	history := []chatMessage{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello there"},
	}
	result := formatChatTemplateFromHistory(history, "how are you", 5)
	expected := "<|im_start|>user\nhi<|im_end|>\n<|im_start|>assistant\nhello there<|im_end|>\n<|im_start|>user\nhow are you"
	assert.Equal(t, expected, result)
}

func TestFormatChatTemplateFromHistory_MaxTurns(t *testing.T) {
	history := []chatMessage{
		{Role: "user", Content: "old message"},
		{Role: "assistant", Content: "old reply"},
		{Role: "user", Content: "recent message"},
		{Role: "assistant", Content: "recent reply"},
	}

	// maxTurns=2 should only include the last 2 history entries
	result := formatChatTemplateFromHistory(history, "new text", 2)
	assert.NotContains(t, result, "old message")
	assert.Contains(t, result, "recent message")
	assert.Contains(t, result, "recent reply")
	assert.Contains(t, result, "new text")
}

func TestFormatChatTemplateFromHistory_LastMessageOpen(t *testing.T) {
	result := formatChatTemplateFromHistory(nil, "yes", 5)
	// The last message should NOT end with <|im_end|>
	assert.True(t, len(result) > 0)
	assert.False(t, result[len(result)-1] == '>')
	assert.NotContains(t, result, "<|im_end|>")
}

func TestFormatChatTemplateFromHistory_SkipsEmptyMessages(t *testing.T) {
	history := []chatMessage{
		{Role: "user", Content: "hi"},
		{Role: "", Content: "skip me"},
		{Role: "assistant", Content: ""},
		{Role: "assistant", Content: "real reply"},
	}
	result := formatChatTemplateFromHistory(history, "test", 10)
	assert.NotContains(t, result, "skip me")
	assert.Contains(t, result, "real reply")
}

// --- history tracking tests ---

func TestLivekitEOS_HistoryFromPackets(t *testing.T) {
	eos := &LivekitEOS{
		history: []chatMessage{},
	}

	// Simulate fire recording a user turn
	eos.history = append(eos.history, chatMessage{Role: "user", Content: "hello"})
	assert.Len(t, eos.history, 1)
	assert.Equal(t, "user", eos.history[0].Role)
	assert.Equal(t, "hello", eos.history[0].Content)

	// Simulate LLMResponseDonePacket recording an assistant turn
	eos.history = append(eos.history, chatMessage{Role: "assistant", Content: "hi there"})
	assert.Len(t, eos.history, 2)
	assert.Equal(t, "assistant", eos.history[1].Role)
}

func TestLivekitEOS_SendAfterClose_DoesNotEnqueueCommand(t *testing.T) {
	eos := &LivekitEOS{
		cmdCh:  make(chan command, 1),
		stopCh: make(chan struct{}),
		state:  &eosState{segment: SpeechSegment{}},
	}
	close(eos.stopCh)

	eos.send(command{fireNow: true})

	assert.Equal(t, 0, len(eos.cmdCh))
}
