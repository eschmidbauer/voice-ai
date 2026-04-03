// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.
package internal_vonage_telephony

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
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

func TestVonageAuth_ValidCredentials(t *testing.T) {
	// vonage.CreateAuthFromAppPrivateKey requires a valid PEM key, so this
	// test verifies the vault parsing path up to the SDK call. We expect an
	// error from the SDK due to the non-PEM key, but NOT a vault-parsing error.
	cred := testVaultCredential(t, map[string]interface{}{
		"private_key":    "not-a-real-pem-key",
		"application_id": "app-123",
	})

	_, err := vonageAuth(cred)
	// The SDK will reject the fake key, but the vault parsing succeeded.
	assert.Error(t, err)
	assert.NotContains(t, err.Error(), "vault credential")
	assert.NotContains(t, err.Error(), "illegal vault config")
}

func TestVonageAuth_NilVaultValue(t *testing.T) {
	cred := &protos.VaultCredential{Value: nil}

	_, err := vonageAuth(cred)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "vault credential value is nil")
}

func TestVonageAuth_MissingPrivateKey(t *testing.T) {
	cred := testVaultCredential(t, map[string]interface{}{
		"application_id": "app-123",
	})

	_, err := vonageAuth(cred)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "privateKey")
}

func TestVonageAuth_MissingApplicationId(t *testing.T) {
	cred := testVaultCredential(t, map[string]interface{}{
		"private_key": "some-key",
	})

	_, err := vonageAuth(cred)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "application_id")
}

// TestReceiveCall tests the ReceiveCall method with Vonage webhook parameters
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
			name: "Valid Vonage webhook with all parameters",
			queryParams: map[string]string{
				"from":              "15703768754",
				"to":                "12019868532",
				"endpoint_type":     "phone",
				"conversation_uuid": "CON-3d4ae1dd-5e14-4131-be3d-0247cb19a28a",
				"uuid":              "bccbc3faaf864e1641fe0cdb1921b6aa",
				"region_url":        "https://api-ap-3.vonage.com",
			},
			expectedError: false,
			expectedPhone: "15703768754",
			checkCallInfo: func(t *testing.T, info *internal_type.CallInfo) {
				require.NotNil(t, info)
				assert.Equal(t, "vonage", info.Provider)
				assert.Equal(t, "SUCCESS", info.Status)
				assert.Equal(t, "15703768754", info.CallerNumber)
				assert.Equal(t, "bccbc3faaf864e1641fe0cdb1921b6aa", info.ChannelUUID)

				// Check StatusInfo
				assert.Equal(t, "webhook", info.StatusInfo.Event)
				assert.NotNil(t, info.StatusInfo.Payload)
				payload, ok := info.StatusInfo.Payload.(map[string]string)
				require.True(t, ok, "Payload should be map[string]string")
				assert.Equal(t, "15703768754", payload["from"])

				// Check Extra for conversation_uuid
				require.NotNil(t, info.Extra)
				assert.Equal(t, "CON-3d4ae1dd-5e14-4131-be3d-0247cb19a28a", info.Extra["conversation_uuid"])
			},
		},
		{
			name: "Valid webhook with minimal parameters",
			queryParams: map[string]string{
				"from": "15703768754",
				"to":   "12019868532",
			},
			expectedError: false,
			expectedPhone: "15703768754",
			checkCallInfo: func(t *testing.T, info *internal_type.CallInfo) {
				require.NotNil(t, info)
				assert.Equal(t, "vonage", info.Provider)
				assert.Equal(t, "SUCCESS", info.Status)
				assert.Equal(t, "webhook", info.StatusInfo.Event)
				assert.NotNil(t, info.StatusInfo.Payload)
				assert.Empty(t, info.ChannelUUID, "ChannelUUID should be empty without uuid param")
			},
		},
		{
			name: "Missing 'from' parameter",
			queryParams: map[string]string{
				"to":                "12019868532",
				"conversation_uuid": "CON-3d4ae1dd-5e14-4131-be3d-0247cb19a28a",
			},
			expectedError: true,
			expectedPhone: "",
			checkCallInfo: func(t *testing.T, info *internal_type.CallInfo) {
				// CallInfo should be nil on error
			},
		},
		{
			name: "Empty 'from' parameter",
			queryParams: map[string]string{
				"from": "",
				"to":   "12019868532",
			},
			expectedError: true,
			expectedPhone: "",
			checkCallInfo: func(t *testing.T, info *internal_type.CallInfo) {
				// CallInfo should be nil on error
			},
		},
		{
			name: "Only conversation_uuid without phone",
			queryParams: map[string]string{
				"conversation_uuid": "CON-3d4ae1dd-5e14-4131-be3d-0247cb19a28a",
				"uuid":              "bccbc3faaf864e1641fe0cdb1921b6aa",
			},
			expectedError: true,
			expectedPhone: "",
			checkCallInfo: func(t *testing.T, info *internal_type.CallInfo) {
				// CallInfo should be nil on error
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

			// Create telephony instance
			telephony := &vonageTelephony{}

			// Call ReceiveCall
			callInfo, err := telephony.ReceiveCall(c)

			// Verify error expectation
			if tt.expectedError {
				assert.Error(t, err)
				assert.Nil(t, callInfo)
			} else {
				assert.NoError(t, err)
				require.NotNil(t, callInfo)
				assert.Equal(t, tt.expectedPhone, callInfo.CallerNumber)
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
		"from":              "15703768754",
		"to":                "12019868532",
		"endpoint_type":     "phone",
		"conversation_uuid": "CON-3d4ae1dd-5e14-4131-be3d-0247cb19a28a",
		"uuid":              "bccbc3faaf864e1641fe0cdb1921b6aa",
		"region_url":        "https://api-ap-3.vonage.com",
		"x-api-key":         "3dd5c2eef53d27942bccd892750fda23ea0b92965d4699e73d8e754ab882955f",
	}

	queryValues := url.Values{}
	for key, value := range queryParams {
		queryValues.Add(key, value)
	}

	req := httptest.NewRequest(http.MethodGet, "/?"+queryValues.Encode(), nil)
	c.Request = req

	telephony := &vonageTelephony{}
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
