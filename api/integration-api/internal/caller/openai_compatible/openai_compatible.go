// Rapida – Open Source Voice AI Orchestration Platform
// Copyright (C) 2023-2025 Prashant Srivastav <prashant@rapida.ai>
// Licensed under a modified GPL-2.0. See the LICENSE file for details.
package internal_openai_compatible_callers

import (
	"errors"
	"fmt"
	"strings"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"

	internal_callers "github.com/rapidaai/api/integration-api/internal/type"
	"github.com/rapidaai/pkg/commons"
	type_enums "github.com/rapidaai/pkg/types/enums"
	integration_api "github.com/rapidaai/protos"
)

type OpenAICompatible struct {
	logger     commons.Logger
	credential internal_callers.CredentialResolver
}

const (
	API_URL = "url"
	API_KEY = "key"
)

const (
	ChatRoleAssistant string = "assistant"
	ChatRoleFunction  string = "function"
	ChatRoleSystem    string = "system"
	ChatRoleTool      string = "tool"
	ChatRoleUser      string = "user"
)

func openAICompatible(logger commons.Logger, credential *integration_api.Credential) OpenAICompatible {
	_credential := credential.GetValue().AsMap()
	return OpenAICompatible{
		logger: logger,
		credential: func() map[string]interface{} {
			return _credential
		},
	}
}

func (oc *OpenAICompatible) GetClient() (*openai.Client, error) {
	credentials := oc.credential()
	rawURL, ok := credentials[API_URL]
	if !ok {
		oc.logger.Errorf("Unable to get base url for openai-compatible client")
		return nil, errors.New("unable to resolve the base url credential")
	}
	baseURL, ok := rawURL.(string)
	if !ok || strings.TrimSpace(baseURL) == "" {
		return nil, errors.New("openai-compatible base url must be a non-empty string")
	}

	opts := []option.RequestOption{
		option.WithBaseURL(baseURL),
	}
	if rawKey, ok := credentials[API_KEY]; ok {
		if apiKey, ok := rawKey.(string); ok && strings.TrimSpace(apiKey) != "" {
			opts = append(opts, option.WithAPIKey(apiKey))
		}
	}

	client := openai.NewClient(opts...)
	return &client, nil
}

func (oc *OpenAICompatible) GetCompletionUsages(usages openai.CompletionUsage) []*integration_api.Metric {
	metrics := make([]*integration_api.Metric, 0)
	metrics = append(metrics, &integration_api.Metric{
		Name:        type_enums.OUTPUT_TOKEN.String(),
		Value:       fmt.Sprintf("%d", usages.CompletionTokens),
		Description: "LLM Output token",
	})

	metrics = append(metrics, &integration_api.Metric{
		Name:        type_enums.INPUT_TOKEN.String(),
		Value:       fmt.Sprintf("%d", usages.PromptTokens),
		Description: "LLM Input Token",
	})

	metrics = append(metrics, &integration_api.Metric{
		Name:        type_enums.TOTAL_TOKEN.String(),
		Value:       fmt.Sprintf("%d", usages.TotalTokens),
		Description: "Total Token",
	})
	return metrics
}
