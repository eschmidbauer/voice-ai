// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

package internal_transformer_google

import (
	"fmt"
	"strings"

	"cloud.google.com/go/speech/apiv2/speechpb"
	"cloud.google.com/go/texttospeech/apiv1/texttospeechpb"
	"github.com/rapidaai/pkg/commons"
	"github.com/rapidaai/pkg/utils"
	"github.com/rapidaai/protos"
	"google.golang.org/api/option"
)

// Introduced constants for default values
const (
	DefaultLanguageCode = "en-US"            // Default language code for Speech-to-Text
	DefaultModel        = "long"             // Default model used for Speech recognition
	DefaultVoice        = "en-US-Chirp-HD-F" // Default voice for Text-to-Speech
)

// googleOption is the primary configuration structure for Google services
type googleOption struct {
	logger       commons.Logger
	clientOptons []option.ClientOption
	mdlOpts      utils.Option
	projectId    string
}

// NewGoogleOption initializes googleOption with provided credentials, audio configurations, and options.
// Improves error handling and logging for better debugging and robustness.
func NewGoogleOption(logger commons.Logger, vaultCredential *protos.VaultCredential, opts utils.Option) (*googleOption, error) {
	co := make([]option.ClientOption, 0)
	var projectID string
	credentialsMap := vaultCredential.GetValue().AsMap()
	if v, ok := credentialsMap["key"]; ok {
		if key, ok := v.(string); ok && key != "" {
			co = append(co, option.WithAPIKey(key))
		}
	}

	if v, ok := credentialsMap["project_id"]; ok {
		if prj, ok := v.(string); ok && prj != "" {
			projectID = prj
			co = append(co, option.WithQuotaProject(prj))
		}
	}

	if v, ok := credentialsMap["service_account_key"]; ok {
		if serviceCrd, ok := v.(string); ok && serviceCrd != "" {
			co = append(co, option.WithCredentialsJSON([]byte(serviceCrd)))
		}
	}

	return &googleOption{
		logger:       logger,
		mdlOpts:      opts,
		clientOptons: co,
		projectId:    projectID,
	}, nil
}

// GetClientOptions returns all configured Google API client options.
func (gO *googleOption) GetClientOptions() []option.ClientOption {
	return gO.clientOptons
}

// SpeechToTextOptions generates a configuration for Google Speech-to-Text streaming recognition.
// Default language and model are used unless overridden via mdlOpts.
func (gog *googleOption) SpeechToTextOptions() *speechpb.StreamingRecognitionConfig {

	opts := &speechpb.StreamingRecognitionConfig{

		Config: &speechpb.RecognitionConfig{
			DecodingConfig: &speechpb.RecognitionConfig_ExplicitDecodingConfig{
				ExplicitDecodingConfig: &speechpb.ExplicitDecodingConfig{
					Encoding:          speechpb.ExplicitDecodingConfig_LINEAR16,
					SampleRateHertz:   16000,
					AudioChannelCount: 1,
				},
			},
			Features: &speechpb.RecognitionFeatures{
				EnableAutomaticPunctuation: true,
				EnableWordConfidence:       true,
				ProfanityFilter:            true,
				EnableSpokenPunctuation:    true,
			},
			LanguageCodes: []string{DefaultLanguageCode},
			Model:         DefaultModel,

			// global// "latest_long, telephony",
			// DenoiserConfig: &speechpb.DenoiserConfig{
			// 	DenoiseAudio: true,
			// },
		},

		StreamingFeatures: &speechpb.StreamingRecognitionFeatures{
			EnableVoiceActivityEvents: false,
			InterimResults:            true,
		},
	}

	if language, err := gog.mdlOpts.GetString("listen.language"); err == nil {
		codes := strings.Split(language, commons.SEPARATOR)
		nonEmptyCodes := []string{}
		for _, code := range codes {
			code = strings.TrimSpace(code)
			if code != "" {
				nonEmptyCodes = append(nonEmptyCodes, code)
			}
		}
		opts.Config.LanguageCodes = nonEmptyCodes
	} else {
		gog.logger.Warn("Language not specified, defaulting to " + DefaultLanguageCode)
	}

	if model, err := gog.mdlOpts.GetString("listen.model"); err == nil {
		opts.Config.Model = model
	} else {
		gog.logger.Warn("Model not specified, defaulting to " + DefaultModel)
	}

	return opts
}

// TextToSpeechOptions generates a configuration for Google Text-to-Speech streaming synthesis.
func (goog *googleOption) TextToSpeechOptions() *texttospeechpb.StreamingSynthesizeConfig {
	options := &texttospeechpb.StreamingSynthesizeConfig{
		Voice: &texttospeechpb.VoiceSelectionParams{
			Name: DefaultVoice,
		},
		StreamingAudioConfig: &texttospeechpb.StreamingAudioConfig{
			AudioEncoding:   texttospeechpb.AudioEncoding_PCM,
			SampleRateHertz: 16000,
		},
	}

	// Override voice configuration if specified in options
	if voice, err := goog.mdlOpts.GetString("speak.voice.id"); err == nil {
		options.Voice.Name = voice
	} else {
		goog.logger.Warn("Voice not specified, defaulting to " + DefaultVoice)
	}

	return options
}

func (gog *googleOption) GetRecognizer() string {
	if region, err := gog.mdlOpts.GetString("listen.region"); err == nil {
		if region != "global" {
			return fmt.Sprintf("projects/%s/locations/%s/recognizers/_", gog.projectId, region)
		}
	}
	return fmt.Sprintf("projects/%s/locations/global/recognizers/_", gog.projectId)
}

func (gog *googleOption) GetSpeechToTextClientOptions() []option.ClientOption {
	if region, err := gog.mdlOpts.GetString("listen.region"); err == nil {
		if region != "global" {
			return append(gog.clientOptons, option.WithEndpoint(fmt.Sprintf("%s-speech.googleapis.com:443", region)))
		}
	}
	return gog.clientOptons
}
