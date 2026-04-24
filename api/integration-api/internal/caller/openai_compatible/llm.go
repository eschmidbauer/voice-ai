package internal_openai_compatible_callers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/shared"

	internal_caller_metrics "github.com/rapidaai/api/integration-api/internal/caller/metrics"
	internal_callers "github.com/rapidaai/api/integration-api/internal/type"
	"github.com/rapidaai/pkg/commons"
	type_enums "github.com/rapidaai/pkg/types/enums"
	"github.com/rapidaai/pkg/utils"
	"github.com/rapidaai/protos"
)

type largeLanguageCaller struct {
	OpenAICompatible
}

func NewLargeLanguageCaller(logger commons.Logger, credential *protos.Credential) internal_callers.LargeLanguageCaller {
	return &largeLanguageCaller{
		OpenAICompatible: openAICompatible(logger, credential),
	}
}

func (llc *largeLanguageCaller) GetChatCompletion(
	ctx context.Context,
	allMessages []*protos.Message,
	options *internal_callers.ChatCompletionOptions,
) (*protos.Message, []*protos.Metric, error) {
	metrics := internal_caller_metrics.NewMetricBuilder(options.RequestId)
	metrics.OnStart()

	client, err := llc.GetClient()
	if err != nil {
		llc.logger.Errorf("chat completion unable to get client for openai-compatible %v", err)
		return nil, metrics.OnFailure().Build(), err
	}

	llmRequest := llc.getChatCompleteParameter(options, false)
	llmRequest.Messages = llc.buildHistory(allMessages)

	options.PreHook(utils.ToJson(llmRequest))

	resp, err := client.Chat.Completions.New(ctx, llmRequest)
	if err != nil {
		llc.logger.Errorf("chat completion failed to get response from openai-compatible %v", err)
		failure := metrics.OnFailure().Build()
		payload := map[string]interface{}{"error": err}
		if resp != nil {
			payload["result"] = resp
		}
		options.PostHook(payload, failure)
		return nil, failure, err
	}

	assistantMsg := &protos.AssistantMessage{
		Contents:  make([]string, 0),
		ToolCalls: make([]*protos.ToolCall, 0),
	}

	for _, choice := range resp.Choices {
		if choice.Message.Content != "" {
			assistantMsg.Contents = append(assistantMsg.Contents, choice.Message.Content)
		}
		for _, tool := range choice.Message.ToolCalls {
			if tool.Type == "function" {
				assistantMsg.ToolCalls = append(assistantMsg.ToolCalls, &protos.ToolCall{
					Id:   tool.ID,
					Type: string(tool.Type),
					Function: &protos.FunctionCall{
						Name:      tool.Function.Name,
						Arguments: tool.Function.Arguments,
					},
				})
			}
		}
	}

	message := &protos.Message{
		Role: ChatRoleAssistant,
		Message: &protos.Message_Assistant{
			Assistant: assistantMsg,
		},
	}

	metrics.OnAddMetrics(llc.GetCompletionUsages(resp.Usage)...)

	options.PostHook(map[string]interface{}{
		"result": resp,
	}, metrics.OnSuccess().Build())
	return message, metrics.Build(), nil
}

func (llc *largeLanguageCaller) StreamChatCompletion(
	ctx context.Context,
	allMessages []*protos.Message,
	options *internal_callers.ChatCompletionOptions,
	onStream func(string, *protos.Message) error,
	onMetrics func(string, *protos.Message, []*protos.Metric) error,
	onError func(string, error),
) error {
	start := time.Now()
	metrics := internal_caller_metrics.NewMetricBuilder(options.RequestId)
	metrics.OnStart()
	var firstTokenTime *time.Time

	client, err := llc.GetClient()
	if err != nil {
		llc.logger.Errorf("chat completion unable to get client for openai-compatible: %v", err)
		onError(options.Request.GetRequestId(), err)
		options.PostHook(map[string]interface{}{
			"error": err,
		}, metrics.OnFailure().Build())
		return err
	}

	completionsOptions := llc.getChatCompleteParameter(options, true)
	completionsOptions.Messages = llc.buildHistory(allMessages)
	options.PreHook(utils.ToJson(completionsOptions))
	llc.logger.Benchmark("openai_compatible.llm.GetChatCompletion.llmRequestPrepare", time.Since(start))

	resp := client.Chat.Completions.NewStreaming(ctx, completionsOptions)
	if resp.Err() != nil {
		llc.logger.Errorf("Failed to get chat completions stream: %v", resp.Err())
		options.PostHook(map[string]interface{}{
			"result": utils.ToJson(resp),
			"error":  resp.Err(),
		}, metrics.OnFailure().Build())
		onError(options.Request.GetRequestId(), resp.Err())
		return resp.Err()
	}
	defer resp.Close()
	assistantMsg := &protos.AssistantMessage{
		Contents:  make([]string, 0),
		ToolCalls: make([]*protos.ToolCall, 0),
	}
	contentBuffer := make([]string, 0)
	hasToolCalls := false

	accumulate := openai.ChatCompletionAccumulator{}
	for resp.Next() {
		chatCompletions := resp.Current()
		accumulate.AddChunk(chatCompletions)

		if tool, ok := accumulate.JustFinishedToolCall(); ok {
			hasToolCalls = true
			assistantMsg.ToolCalls = append(assistantMsg.ToolCalls, &protos.ToolCall{
				Id:   tool.ID,
				Type: "function",
				Function: &protos.FunctionCall{
					Name:      tool.Name,
					Arguments: tool.Arguments,
				},
			})
		}

		for i, choice := range chatCompletions.Choices {
			if len(choice.Delta.ToolCalls) > 0 {
				hasToolCalls = true
			}

			content := choice.Delta.Content
			if content != "" {
				if len(contentBuffer) <= i {
					contentBuffer = append(contentBuffer, content)
				} else {
					contentBuffer[i] += content
				}

				if !hasToolCalls {
					if firstTokenTime == nil {
						now := time.Now()
						firstTokenTime = &now
					}
					tokenMsg := &protos.Message{
						Role: ChatRoleAssistant,
						Message: &protos.Message_Assistant{
							Assistant: &protos.AssistantMessage{
								Contents: []string{content},
							},
						},
					}
					if err := onStream(options.Request.GetRequestId(), tokenMsg); err != nil {
						llc.logger.Warnf("error streaming token: %v", err)
					}
				}
			}
		}
	}

	if resp.Err() != nil {
		llc.logger.Errorf("Failed while reading chat completions stream: %v", resp.Err())
		options.PostHook(map[string]interface{}{
			"result": utils.ToJson(resp),
			"error":  resp.Err(),
		}, metrics.OnFailure().Build())
		onError(options.Request.GetRequestId(), resp.Err())
		return resp.Err()
	}

	assistantMsg.Contents = contentBuffer
	protoMsg := &protos.Message{
		Role: ChatRoleAssistant,
		Message: &protos.Message_Assistant{
			Assistant: assistantMsg,
		},
	}

	metrics.OnAddMetrics(llc.GetCompletionUsages(accumulate.Usage)...)

	if firstTokenTime != nil {
		metrics.OnAddMetrics(&protos.Metric{
			Name:        type_enums.TIME_TO_FIRST_TOKEN.String(),
			Value:       fmt.Sprintf("%d", firstTokenTime.Sub(start)),
			Description: "Time to receive first token from LLM",
		})
	}
	metrics.OnSuccess()
	onMetrics(options.Request.GetRequestId(), protoMsg, metrics.Build())
	options.PostHook(map[string]interface{}{
		"result": utils.ToJson(accumulate),
	}, metrics.Build())

	return nil
}

func (llc *largeLanguageCaller) buildHistory(allMessages []*protos.Message) []openai.ChatCompletionMessageParamUnion {
	msg := make([]openai.ChatCompletionMessageParamUnion, 0)
	for _, cntn := range allMessages {
		switch cntn.GetRole() {
		case ChatRoleUser:
			if user := cntn.GetUser(); user != nil {
				msg = append(msg, openai.UserMessage(user.GetContent()))
			}
		case ChatRoleAssistant:
			if assistant := cntn.GetAssistant(); assistant != nil {
				txtContent := strings.Join(assistant.GetContents(), "")
				toolCalls := assistant.GetToolCalls()
				assistantMessage := openai.ChatCompletionAssistantMessageParam{}
				if len(txtContent) > 0 || len(toolCalls) > 0 {
					if len(txtContent) > 0 {
						assistantMessage.Content = openai.ChatCompletionAssistantMessageParamContentUnion{
							OfString: openai.String(txtContent),
						}
					}
					if len(toolCalls) > 0 {
						fctCall := make([]openai.ChatCompletionMessageToolCallParam, 0)
						for _, ttc := range toolCalls {
							fctCall = append(fctCall, openai.ChatCompletionMessageToolCallParam{
								ID: ttc.GetId(),
								Function: openai.ChatCompletionMessageToolCallFunctionParam{
									Name:      ttc.GetFunction().GetName(),
									Arguments: ttc.GetFunction().GetArguments(),
								},
							})
						}
						assistantMessage.ToolCalls = fctCall
					}
					msg = append(msg, openai.ChatCompletionMessageParamUnion{
						OfAssistant: &assistantMessage,
					})
				}
			}

		case ChatRoleSystem:
			if system := cntn.GetSystem(); system != nil {
				txtContent := system.GetContent()
				if len(txtContent) > 0 {
					msg = append(msg, openai.SystemMessage(txtContent))
				}
			}

		case ChatRoleTool:
			if tool := cntn.GetTool(); tool != nil {
				for _, t := range tool.GetTools() {
					msg = append(msg, openai.ToolMessage(t.GetContent(), t.GetId()))
				}
			}
		}
	}
	return msg
}

func (llc *largeLanguageCaller) getChatCompleteParameter(
	opts *internal_callers.ChatCompletionOptions,
	streaming bool,
) openai.ChatCompletionNewParams {
	options := openai.ChatCompletionNewParams{}
	if streaming {
		options.StreamOptions = openai.ChatCompletionStreamOptionsParam{
			IncludeUsage: openai.Bool(true),
		}
	}
	if len(opts.ToolDefinitions) > 0 {
		fns := make([]openai.ChatCompletionToolParam, 0, len(opts.ToolDefinitions))
		for _, tl := range opts.ToolDefinitions {
			if tl.Type != "function" {
				continue
			}
			fn := tl.Function
			if fn == nil {
				continue
			}
			funcDef := openai.FunctionDefinitionParam{
				Name: fn.Name,
			}
			if fn.Description != "" {
				funcDef.Description = openai.String(fn.Description)
			}
			if fn.Parameters != nil {
				funcDef.Parameters = fn.Parameters.ToMap()
			} else {
				funcDef.Parameters = map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
				}
			}
			fns = append(fns, openai.ChatCompletionToolParam{
				Function: funcDef,
			})
		}
		options.Tools = fns
	}

	for key, value := range opts.ModelParameter {
		switch key {
		case "model.name":
			if modelName, err := utils.AnyToString(value); err == nil {
				options.Model = modelName
			}
		case "model.user":
			if user, err := utils.AnyToString(value); err == nil {
				options.User = openai.String(user)
			}
		case "model.reasoning_effort":
			if re, err := utils.AnyToString(value); err == nil {
				options.ReasoningEffort = shared.ReasoningEffort(re)
			}
		case "model.seed":
			if seed, err := utils.AnyToInt64(value); err == nil {
				options.Seed = openai.Int(seed)
			}
		case "model.top_logprobs":
			if tl, err := utils.AnyToInt64(value); err == nil {
				options.TopLogprobs = openai.Int(tl)
			}
		case "model.metadata":
			format, _ := utils.AnyToString(value)
			var mtd map[string]string
			if err := json.Unmarshal([]byte(format), &mtd); err == nil {
				options.Metadata = shared.Metadata(mtd)
			}
		case "model.frequency_penalty":
			if fp, err := utils.AnyToFloat64(value); err == nil {
				options.FrequencyPenalty = openai.Float(fp)
			}
		case "model.temperature":
			if temp, err := utils.AnyToFloat64(value); err == nil {
				options.Temperature = openai.Float(temp)
			}
		case "model.top_p":
			if topP, err := utils.AnyToFloat64(value); err == nil {
				options.TopP = openai.Float(topP)
			}
		case "model.presence_penalty":
			if pp, err := utils.AnyToFloat64(value); err == nil {
				options.PresencePenalty = openai.Float(pp)
			}
		case "model.max_completion_tokens":
			if maxTokens, err := utils.AnyToInt64(value); err == nil {
				options.MaxTokens = openai.Int(maxTokens)
			}
		case "model.stop":
			if stopStr, err := utils.AnyToString(value); err == nil {
				for _, stopper := range strings.Split(stopStr, ",") {
					if strings.TrimSpace(stopper) != "" {
						options.Stop.OfStringArray = append(options.Stop.OfStringArray, stopper)
					}
				}
			}
		case "model.tool_choice":
			if choice, err := utils.AnyToString(value); err == nil && len(opts.ToolDefinitions) > 0 {
				switch choice {
				case "auto":
					options.ToolChoice = openai.ChatCompletionToolChoiceOptionUnionParam{
						OfAuto: openai.String("auto"),
					}
				case "required":
					options.ToolChoice = openai.ChatCompletionToolChoiceOptionUnionParam{
						OfAuto: openai.String("required"),
					}
				case "none":
					options.ToolChoice = openai.ChatCompletionToolChoiceOptionUnionParam{
						OfAuto: openai.String("none"),
					}
				default:
					llc.logger.Warnf("openai-compatible: unrecognized model.tool_choice value %q; leaving tool_choice unset", choice)
				}
			}
		case "model.response_format":
			if format, err := utils.AnyToJSON(value); err == nil {
				if _type, ok := format["type"]; ok {
					switch _type.(string) {
					case "json_object":
						options.ResponseFormat = openai.ChatCompletionNewParamsResponseFormatUnion{
							OfJSONObject: &openai.ResponseFormatJSONObjectParam{},
						}
					case "text":
						textParam := shared.NewResponseFormatTextParam()
						options.ResponseFormat = openai.ChatCompletionNewParamsResponseFormatUnion{
							OfText: &textParam,
						}
					case "json_schema":
						if schemaData, ok := format["json_schema"].(map[string]interface{}); ok {
							jsonSchemaParam := shared.ResponseFormatJSONSchemaJSONSchemaParam{}
							jsonData, err := json.Marshal(schemaData)
							if err != nil {
								llc.logger.Warnf("openai-compatible: failed to marshal json_schema: %v", err)
								break
							}
							if err := json.Unmarshal(jsonData, &jsonSchemaParam); err != nil {
								llc.logger.Warnf("openai-compatible: failed to unmarshal json_schema: %v", err)
								break
							}
							options.ResponseFormat = openai.ChatCompletionNewParamsResponseFormatUnion{
								OfJSONSchema: &shared.ResponseFormatJSONSchemaParam{
									JSONSchema: jsonSchemaParam,
								},
							}
						}
					}
				}
			}
		}
	}
	return options
}
