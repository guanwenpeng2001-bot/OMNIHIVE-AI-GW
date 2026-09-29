package service

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTPMUsage_NormalizedCacheTokensCountOnce(t *testing.T) {
	tokens, err := TPMUsageTokens(UsageTokens{InputTokens: 12, OutputTokens: 8, CacheCreationTokens: 20, CacheReadTokens: 30, CacheCreation5mTokens: 10, CacheCreation1hTokens: 10, ImageCacheReadTokens: 15})
	require.NoError(t, err)
	require.Equal(t, int64(70), tokens)
}

func TestTPMConfig_OmittedAndZeroAreDifferent(t *testing.T) {
	var request struct {
		Limit OptionalTPMLimit `json:"tpm_limit"`
	}
	require.NoError(t, json.Unmarshal([]byte(`{}`), &request))
	require.Nil(t, request.Limit.Pointer())
	require.NoError(t, json.Unmarshal([]byte(`{"tpm_limit":0}`), &request))
	require.NotNil(t, request.Limit.Pointer())
	require.Zero(t, *request.Limit.Pointer())
}

func TestTPMConfig_RejectsNullNegativeFractionAndOverflow(t *testing.T) {
	for _, payload := range []string{`null`, `-1`, `1.5`, `"1"`, `9223372036854775808`, `true`, `[]`, `1e2`} {
		var request struct {
			Limit OptionalTPMLimit `json:"tpm_limit"`
		}
		require.Error(t, json.Unmarshal([]byte(`{"tpm_limit":`+payload+`}`), &request), payload)
	}
	var request struct {
		Limit OptionalTPMLimit `json:"tpm_limit"`
	}
	require.NoError(t, json.Unmarshal([]byte(`{"tpm_limit":9223372036854775807}`), &request))
	require.Equal(t, int64(math.MaxInt64), request.Limit.Value)
}

func TestTPMEstimate_RejectsUnknownBound(t *testing.T) {
	_, err := (TPMEstimate{Candidates: 1}).Tokens()
	require.ErrorIs(t, err, ErrTPMEstimateUnavailable)
	tokens, err := (TPMEstimate{Tokenless: true}).Tokens()
	require.NoError(t, err)
	require.Zero(t, tokens)
	_, err = (TPMEstimate{Tokenless: true, InputBound: 1}).Tokens()
	require.ErrorIs(t, err, ErrTPMEstimateUnavailable)
}

func TestTPMEstimate_OverflowAndMultipleCandidates(t *testing.T) {
	tokens, err := (TPMEstimate{InputBound: 30, OutputBound: 10, Candidates: 3, Verified: true}).Tokens()
	require.NoError(t, err)
	require.Equal(t, int64(60), tokens)
	for _, estimate := range []TPMEstimate{
		{InputBound: math.MaxInt64, OutputBound: 1, Candidates: 1, Verified: true},
		{OutputBound: math.MaxInt64, Candidates: 2, Verified: true},
		{InputBound: -1, Candidates: 1, Verified: true},
		{OutputBound: -1, Candidates: 1, Verified: true},
		{Candidates: 0, Verified: true},
	} {
		_, err := estimate.Tokens()
		require.ErrorIs(t, err, ErrTPMEstimateUnavailable)
	}
	tokens, err = (TPMEstimate{InputBound: math.MaxInt64, Candidates: 1, Verified: true}).Tokens()
	require.NoError(t, err)
	require.Equal(t, int64(math.MaxInt64), tokens)
}
