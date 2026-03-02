// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

package rime_internal

type RimeTextToSpeechResponse struct {
	Type      string `json:"type"`
	Data      string `json:"data"`
	ContextId string `json:"contextId"`
	Message   string `json:"message"`
}
