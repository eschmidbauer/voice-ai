// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.
package internal_exotel_telephony

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rapidaai/api/assistant-api/config"
	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
	"github.com/rapidaai/pkg/utils"
	"github.com/rapidaai/protos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/structpb"
)

func testVaultCredential(t *testing.T, values map[string]interface{}) *protos.VaultCredential {
	t.Helper()
	v, err := structpb.NewStruct(values)
	if err != nil {
		t.Fatalf("failed to create vault credential: %v", err)
	}
	return &protos.VaultCredential{Value: v}
}

func TestClientUrl_ValidCredentials(t *testing.T) {
	cred := testVaultCredential(t, map[string]interface{}{
		"account_sid":   "exotel-sid",
		"client_id":     "client-id",
		"client_secret": "secret-token",
	})
	exo := &exotelTelephony{}

	result, err := exo.ClientUrl(cred, utils.Option{})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Contains(t, *result, "client-id:secret-token")
	assert.Contains(t, *result, "exotel-sid")
}

func TestClientUrl_NilVaultValue(t *testing.T) {
	cred := &protos.VaultCredential{Value: nil}
	exo := &exotelTelephony{}

	result, err := exo.ClientUrl(cred, utils.Option{})
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "vault credential value is nil")
}

func TestClientUrl_MissingAccountSid(t *testing.T) {
	cred := testVaultCredential(t, map[string]interface{}{
		"client_id":     "client-id",
		"client_secret": "secret-token",
	})
	exo := &exotelTelephony{}

	result, err := exo.ClientUrl(cred, utils.Option{})
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "accountSid")
}

func TestAppUrl_NilVaultValue(t *testing.T) {
	cred := &protos.VaultCredential{Value: nil}
	exo := &exotelTelephony{}
	opts := utils.Option{"app_id": "test-app"}

	result, err := exo.AppUrl(cred, opts)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "vault credential value is nil")
}

func TestAppUrl_ValidCredentials(t *testing.T) {
	cred := testVaultCredential(t, map[string]interface{}{
		"account_sid": "exotel-sid",
	})
	exo := &exotelTelephony{}
	opts := utils.Option{"app_id": "test-app"}

	result, err := exo.AppUrl(cred, opts)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Contains(t, *result, "exotel-sid")
	assert.Contains(t, *result, "test-app")
}

func TestAppUrl_MissingAppId(t *testing.T) {
	cred := testVaultCredential(t, map[string]interface{}{
		"account_sid": "exotel-sid",
	})
	exo := &exotelTelephony{}

	result, err := exo.AppUrl(cred, utils.Option{})
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "app_id")
}

// TestReceiveCall tests the ReceiveCall method with Exotel webhook parameters
func TestReceiveCall(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name          string
		queryParams   map[string]string
		expectedError bool
		expectedPhone string
		checkCallInfo func(*testing.T, *internal_type.CallInfo)
	}{
		{
			name: "Valid Exotel inbound webhook with all parameters",
			queryParams: map[string]string{
				"CallSid":  "exotel-call-sid-12345",
				"CallFrom": "+919876543210",
				"CallTo":   "+911234567890",
				"Status":   "ringing",
			},
			expectedError: false,
			expectedPhone: "+919876543210",
			checkCallInfo: func(t *testing.T, info *internal_type.CallInfo) {
				require.NotNil(t, info)
				assert.Equal(t, "exotel", info.Provider)
				assert.Equal(t, "SUCCESS", info.Status)
				assert.Equal(t, "+919876543210", info.CallerNumber)
				assert.Equal(t, "exotel-call-sid-12345", info.ChannelUUID)

				// Check StatusInfo
				assert.Equal(t, "webhook", info.StatusInfo.Event)
				assert.NotNil(t, info.StatusInfo.Payload)
				payload, ok := info.StatusInfo.Payload.(map[string]string)
				require.True(t, ok, "Payload should be map[string]string")
				assert.Equal(t, "+919876543210", payload["CallFrom"])
				assert.Equal(t, "exotel-call-sid-12345", payload["CallSid"])
			},
		},
		{
			name: "Valid webhook with minimal parameters",
			queryParams: map[string]string{
				"CallFrom": "+919876543210",
				"CallTo":   "+911234567890",
			},
			expectedError: false,
			expectedPhone: "+919876543210",
			checkCallInfo: func(t *testing.T, info *internal_type.CallInfo) {
				require.NotNil(t, info)
				assert.Equal(t, "exotel", info.Provider)
				assert.Equal(t, "SUCCESS", info.Status)
				assert.Empty(t, info.ChannelUUID, "ChannelUUID should be empty without CallSid")
				assert.Equal(t, "webhook", info.StatusInfo.Event)
				assert.NotNil(t, info.StatusInfo.Payload)
			},
		},
		{
			name: "Missing 'CallFrom' parameter",
			queryParams: map[string]string{
				"CallTo":  "+911234567890",
				"CallSid": "exotel-call-sid-12345",
			},
			expectedError: true,
			expectedPhone: "",
			checkCallInfo: func(t *testing.T, info *internal_type.CallInfo) {
				// CallInfo should be nil on error
			},
		},
		{
			name: "Empty 'CallFrom' parameter",
			queryParams: map[string]string{
				"CallFrom": "",
				"CallTo":   "+911234567890",
			},
			expectedError: true,
			expectedPhone: "",
			checkCallInfo: func(t *testing.T, info *internal_type.CallInfo) {
				// CallInfo should be nil on error
			},
		},
		{
			name: "Outbound call with CustomField triggers redirect",
			queryParams: map[string]string{
				"CustomField": "v1/talk/exotel/ctx/abc123",
				"CallFrom":    "+919876543210",
			},
			expectedError: false, // No error -- provider already wrote the response
			expectedPhone: "",
			checkCallInfo: func(t *testing.T, info *internal_type.CallInfo) {
				// CallInfo is nil when CustomField is present (outbound redirect)
				assert.Nil(t, info)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test context
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			// Build query string
			queryValues := url.Values{}
			for key, value := range tt.queryParams {
				queryValues.Add(key, value)
			}

			// Create request with query parameters
			req := httptest.NewRequest(http.MethodGet, "/?"+queryValues.Encode(), nil)
			c.Request = req

			// Create telephony instance with config (needed for CustomField path)
			telephony := &exotelTelephony{appCfg: &config.AssistantConfig{PublicAssistantHost: "test.example.com"}}

			// Call ReceiveCall
			callInfo, err := telephony.ReceiveCall(c)

			// Verify error expectation
			if tt.expectedError {
				assert.Error(t, err)
				assert.Nil(t, callInfo)
			} else {
				assert.NoError(t, err)
				if callInfo != nil {
					assert.Equal(t, tt.expectedPhone, callInfo.CallerNumber)
				}
			}

			// Check CallInfo
			if tt.checkCallInfo != nil {
				tt.checkCallInfo(t, callInfo)
			}
		})
	}
}

// TestReceiveCall_QueryParameterExtraction tests that all query parameters are captured in CallInfo payload
func TestReceiveCall_QueryParameterExtraction(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	queryParams := map[string]string{
		"CallSid":    "exotel-call-sid-12345",
		"CallFrom":   "+919876543210",
		"CallTo":     "+911234567890",
		"Direction":  "incoming",
		"Status":     "ringing",
		"AccountSid": "exotel-account-123",
	}

	queryValues := url.Values{}
	for key, value := range queryParams {
		queryValues.Add(key, value)
	}

	req := httptest.NewRequest(http.MethodGet, "/?"+queryValues.Encode(), nil)
	c.Request = req

	telephony := &exotelTelephony{}
	callInfo, err := telephony.ReceiveCall(c)

	require.NoError(t, err)
	require.NotNil(t, callInfo)

	// Verify StatusInfo contains webhook event with all query parameters as payload
	assert.Equal(t, "webhook", callInfo.StatusInfo.Event)
	require.NotNil(t, callInfo.StatusInfo.Payload, "StatusInfo payload should not be nil")

	payloadMap, ok := callInfo.StatusInfo.Payload.(map[string]string)
	require.True(t, ok, "Payload should be map[string]string")

	for key, expectedValue := range queryParams {
		actualValue, exists := payloadMap[key]
		assert.True(t, exists, "Query param '%s' should be in payload", key)
		assert.Equal(t, expectedValue, actualValue, "Value for '%s' should match", key)
	}
}

// TestReceiveCall_OutboundRedirect tests that CustomField triggers outbound redirect response
func TestReceiveCall_OutboundRedirect(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	queryValues := url.Values{}
	queryValues.Add("CustomField", "v1/talk/exotel/ctx/test-context-123")

	req := httptest.NewRequest(http.MethodGet, "/?"+queryValues.Encode(), nil)
	c.Request = req

	telephony := &exotelTelephony{appCfg: &config.AssistantConfig{PublicAssistantHost: "test.example.com"}}
	callInfo, err := telephony.ReceiveCall(c)

	assert.NoError(t, err)
	assert.Nil(t, callInfo)
}

// TestReceiveCall_CallInfoStructure tests the structure of CallInfo data
func TestReceiveCall_CallInfoStructure(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	queryValues := url.Values{}
	queryValues.Add("CallFrom", "+919876543210")
	queryValues.Add("CallTo", "+911234567890")
	queryValues.Add("CallSid", "exotel-call-sid-12345")
	queryValues.Add("Status", "ringing")

	req := httptest.NewRequest(http.MethodGet, "/?"+queryValues.Encode(), nil)
	c.Request = req

	telephony := &exotelTelephony{}
	callInfo, err := telephony.ReceiveCall(c)

	require.NoError(t, err)
	require.NotNil(t, callInfo)

	// Verify CallInfo fields
	assert.Equal(t, "exotel", callInfo.Provider)
	assert.Equal(t, "SUCCESS", callInfo.Status)
	assert.Equal(t, "+919876543210", callInfo.CallerNumber)
	assert.Equal(t, "exotel-call-sid-12345", callInfo.ChannelUUID)
	assert.Empty(t, callInfo.ErrorMessage)

	// Verify StatusInfo
	assert.Equal(t, "webhook", callInfo.StatusInfo.Event)
	assert.NotNil(t, callInfo.StatusInfo.Payload)
}
