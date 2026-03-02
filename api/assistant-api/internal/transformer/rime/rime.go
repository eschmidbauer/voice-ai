// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

package internal_transformer_rime

import (
	"fmt"
	"net/url"

	"github.com/rapidaai/pkg/commons"
	"github.com/rapidaai/pkg/utils"
	"github.com/rapidaai/protos"
)

const (
	RIME_DEFAULT_VOICE = "river"
	RIME_DEFAULT_MODEL = "mistv2"
	RIME_DEFAULT_LANG  = "eng"
)

type rimeOption struct {
	key     string
	logger  commons.Logger
	mdlOpts utils.Option
}

func NewRimeOption(logger commons.Logger, vaultCredential *protos.VaultCredential,
	opts utils.Option) (*rimeOption, error) {
	cx, ok := vaultCredential.GetValue().AsMap()["key"]
	if !ok {
		return nil, fmt.Errorf("rime: illegal vault config")
	}
	return &rimeOption{
		key:     cx.(string),
		mdlOpts: opts,
		logger:  logger,
	}, nil
}

func (co *rimeOption) GetKey() string {
	return co.key
}

func (co *rimeOption) GetTextToSpeechConnectionString() string {
	params := url.Values{}
	params.Add("audioFormat", "pcm")
	params.Add("samplingRate", "16000")

	voice := RIME_DEFAULT_VOICE
	if voiceIDValue, err := co.mdlOpts.GetString("speak.voice.id"); err == nil && voiceIDValue != "" {
		voice = voiceIDValue
	}
	params.Add("speaker", voice)

	model := RIME_DEFAULT_MODEL
	if modelValue, err := co.mdlOpts.GetString("speak.model"); err == nil && modelValue != "" {
		model = modelValue
	}
	params.Add("modelId", model)

	lang := RIME_DEFAULT_LANG
	if langValue, err := co.mdlOpts.GetString("speak.language"); err == nil && langValue != "" {
		lang = langValue
	}
	params.Add("lang", lang)

	if speedAlpha, err := co.mdlOpts.GetString("speak.speed_alpha"); err == nil && speedAlpha != "" {
		params.Add("speedAlpha", speedAlpha)
	}

	return fmt.Sprintf("wss://users-ws.rime.ai/ws2?%s", params.Encode())
}
