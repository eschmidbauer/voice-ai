package adapter_internal

import (
	"context"
	"fmt"
	"strings"
	"testing"

	assistant_config "github.com/rapidaai/api/assistant-api/config"
	internal_assistant_entity "github.com/rapidaai/api/assistant-api/internal/entity/assistants"
	internal_conversation_entity "github.com/rapidaai/api/assistant-api/internal/entity/conversations"
	internal_telemetry_entity "github.com/rapidaai/api/assistant-api/internal/entity/telemetry"
	observe "github.com/rapidaai/api/assistant-api/internal/observe"
	"github.com/rapidaai/pkg/commons"
	gorm_model "github.com/rapidaai/pkg/models/gorm"
	"github.com/rapidaai/pkg/types"
	type_enums "github.com/rapidaai/pkg/types/enums"
	"github.com/rapidaai/protos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func requestorTelemetryTestLogger(t *testing.T) commons.Logger {
	t.Helper()
	logger, err := commons.NewApplicationLogger(
		commons.Name("requestor-telemetry-test"),
		commons.Level("error"),
		commons.EnableFile(false),
	)
	require.NoError(t, err)
	return logger
}

func requestorForTelemetryTest(t *testing.T, providers []*internal_telemetry_entity.AssistantTelemetryProvider) *genericRequestor {
	t.Helper()
	projectID := uint64(11)
	orgID := uint64(22)

	return &genericRequestor{
		logger: requestorTelemetryTestLogger(t),
		config: &assistant_config.AssistantConfig{},
		auth: &types.ServiceScope{
			ProjectId:      &projectID,
			OrganizationId: &orgID,
		},
		assistant: &internal_assistant_entity.Assistant{
			Audited:                     gorm_model.Audited{Id: 101},
			AssistantTelemetryProviders: providers,
		},
		assistantConversation: &internal_conversation_entity.AssistantConversation{
			Audited: gorm_model.Audited{Id: 202},
		},
	}
}

func TestInitializeCollectors_NoProvidersConfigured_UsesNoopCollectors(t *testing.T) {
	r := requestorForTelemetryTest(t, nil)

	r.initializeCollectors(context.Background())

	assert.True(t, strings.Contains(fmt.Sprintf("%T", r.events), "noopEventCollector"))
	assert.True(t, strings.Contains(fmt.Sprintf("%T", r.metrics), "noopMetricCollector"))
	assert.NotPanics(t, func() {
		r.events.Collect(context.Background(), sessionEventRecord("connected"))
		r.metrics.Collect(context.Background(), conversationMetricRecord("202"))
		r.events.Shutdown(context.Background())
		r.metrics.Shutdown(context.Background())
	})
}

func TestInitializeCollectors_LoggingProvider_UsesFanoutCollectors(t *testing.T) {
	r := requestorForTelemetryTest(t, []*internal_telemetry_entity.AssistantTelemetryProvider{
		{
			ProviderType: "logging",
			Enabled:      true,
		},
	})
	r.initializeCollectors(context.Background())
	assert.True(t, strings.Contains(fmt.Sprintf("%T", r.events), "fanoutEventCollector"))
	assert.True(t, strings.Contains(fmt.Sprintf("%T", r.metrics), "fanoutMetricCollector"))
	assert.NotPanics(t, func() {
		r.events.Collect(context.Background(), sessionEventRecord("connected"))
		r.metrics.Collect(context.Background(), conversationMetricRecord("202"))
		r.events.Shutdown(context.Background())
		r.metrics.Shutdown(context.Background())
	})
}

func TestInitializeCollectors_OTLPMissingEndpoint_SkipsToNoopCollectors(t *testing.T) {
	r := requestorForTelemetryTest(t, []*internal_telemetry_entity.AssistantTelemetryProvider{
		{
			ProviderType: "otlp_http",
			Enabled:      true,
		},
	})

	r.initializeCollectors(context.Background())

	assert.True(t, strings.Contains(fmt.Sprintf("%T", r.events), "noopEventCollector"))
	assert.True(t, strings.Contains(fmt.Sprintf("%T", r.metrics), "noopMetricCollector"))
}

func sessionEventRecord(eventType string) observe.EventRecord {
	return observe.EventRecord{Name: "session", Data: map[string]string{"type": eventType}}
}

func conversationMetricRecord(conversationID string) observe.ConversationMetricRecord {
	return observe.ConversationMetricRecord{
		ConversationID: conversationID,
		Metrics: []*protos.Metric{
			{
				Name:  type_enums.STATUS.String(),
				Value: type_enums.RECORD_IN_PROGRESS.String(),
			},
		},
	}
}

func TestInitializeCollectors_UnknownProvider_SkipsToNoopCollectors(t *testing.T) {
	r := requestorForTelemetryTest(t, []*internal_telemetry_entity.AssistantTelemetryProvider{
		{
			ProviderType: "unknown_provider",
			Enabled:      true,
		},
	})
	r.initializeCollectors(context.Background())
	assert.True(t, strings.Contains(fmt.Sprintf("%T", r.events), "noopEventCollector"))
	assert.True(t, strings.Contains(fmt.Sprintf("%T", r.metrics), "noopMetricCollector"))
}
