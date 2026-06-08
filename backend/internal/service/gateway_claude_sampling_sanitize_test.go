package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestSanitizeDeprecatedClaudeSamplingParams_StripsTemperatureForUnsupportedOpus(t *testing.T) {
	body := []byte(`{"model":"claude-opus-4-7","temperature":0.7,"max_tokens":200,"messages":[{"role":"user","content":"hi"}]}`)

	out, changed := sanitizeDeprecatedClaudeSamplingParams(body, "")

	require.True(t, changed)
	require.False(t, gjson.GetBytes(out, "temperature").Exists())
	require.Equal(t, "claude-opus-4-7", gjson.GetBytes(out, "model").String())
	require.Equal(t, int64(200), gjson.GetBytes(out, "max_tokens").Int())
}

func TestSanitizeDeprecatedClaudeSamplingParams_StripsTemperatureForMappedProviderModel(t *testing.T) {
	body := []byte(`{"model":"anthropic/claude-opus-4.8","temperature":1,"messages":[{"role":"user","content":"hi"}]}`)

	out, changed := sanitizeDeprecatedClaudeSamplingParams(body, "")

	require.True(t, changed)
	require.False(t, gjson.GetBytes(out, "temperature").Exists())
}

func TestSanitizeDeprecatedClaudeSamplingParams_PreservesTemperatureForOtherClaudeModels(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-4-6","temperature":0.7,"messages":[{"role":"user","content":"hi"}]}`)

	out, changed := sanitizeDeprecatedClaudeSamplingParams(body, "")

	require.False(t, changed)
	require.Equal(t, 0.7, gjson.GetBytes(out, "temperature").Float())
}
