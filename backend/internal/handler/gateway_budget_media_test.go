//go:build unit

package handler

import (
	"encoding/binary"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGatewayBudgetExtractsMediaUnits(t *testing.T) {
	for _, tc := range []struct {
		protocol, route, body string
		want                  service.BudgetMeter
	}{
		{"images", "/v1/images/generations", `{"n":3,"size":"2048x2048","quality":"high"}`, service.BudgetMeter{Unit: "image", Count: 3, Numerator: 1, Denominator: 1, Size: "2048x2048", Quality: "high"}},
		{"images", "/v1/images/generations", `{"size":"3840x2160"}`, service.BudgetMeter{Unit: "image", Count: 1, Numerator: 1, Denominator: 1, Size: "3840x2160"}},
		{"images", "/v1/images/edits", `{"resolution":"4k","size":"3840x2160"}`, service.BudgetMeter{Unit: "image", Count: 1, Numerator: 1, Denominator: 1, Size: "3840x2160"}},
		{"audio", "/v1/audio/speech", `{"input":"猫🐝ab"}`, service.BudgetMeter{Unit: "character", Count: 1, Numerator: 4, Denominator: 1}},
		{"media", "/v1/videos", `{"duration":8,"resolution":"720p","n":2}`, service.BudgetMeter{Unit: "video_second", Count: 2, Numerator: 8, Denominator: 1, Size: "720p"}},
		{"audio", "/v1/audio/transcriptions", `{"duration_seconds":1,"audio_url":"https://example.test/long"}`, service.BudgetMeter{Unit: "second", Count: 1}},
	} {
		t.Run(tc.route, func(t *testing.T) {
			plan := service.NewGatewayRequestPlan(service.GatewayRequestSpec{Route: tc.route, Protocol: tc.protocol})
			estimate := gatewayRequestBudgetEstimate(plan, &service.APIKey{}, "model", []byte(tc.body))
			require.Equal(t, tc.want, *estimate.Meter)
			require.Zero(t, estimate.InputTokens)
			require.Zero(t, estimate.OutputTokens)
		})
	}
}

func TestGrokMediaBudgetUsesForwardedUnits(t *testing.T) {
	for _, tc := range []struct {
		name, route, body, size string
		characters              int64
	}{
		{name: "tts uses input", route: "/v1/audio/speech", body: `{"input":"猫🐝ab"}`, characters: 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			meter := requestMediaMeter(service.GatewayRequestSpec{Route: tc.route}, []byte(tc.body))
			require.Equal(t, tc.characters, meter.Numerator)
			require.Equal(t, tc.size, meter.Size)
		})
	}
}

func TestGrokMediaBudgetRejectsContradictoryAliases(t *testing.T) {
	for _, tc := range []struct{ route, body string }{
		{route: "/v1/audio/speech", body: `{"input":"short","text":"different"}`},
		{route: "/v1/images/generations", body: `{"size":"1024x1024","resolution":"2k"}`},
	} {
		meter := requestMediaMeter(service.GatewayRequestSpec{Route: tc.route}, []byte(tc.body))
		require.Zero(t, meter.Numerator)
	}
}
func TestPCMTranscriptionBoundUsesActualFrames(t *testing.T) {
	wav := make([]byte, 44+24000)
	copy(wav, "RIFF")
	binary.LittleEndian.PutUint32(wav[4:], uint32(len(wav)-8))
	copy(wav[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(wav[16:], 16)
	binary.LittleEndian.PutUint16(wav[20:], 1)
	binary.LittleEndian.PutUint16(wav[22:], 1)
	binary.LittleEndian.PutUint32(wav[24:], 8000)
	binary.LittleEndian.PutUint32(wav[28:], 16000)
	binary.LittleEndian.PutUint16(wav[32:], 2)
	binary.LittleEndian.PutUint16(wav[34:], 16)
	copy(wav[36:], "data")
	binary.LittleEndian.PutUint32(wav[40:], 24000)
	m := requestMediaMeter(service.GatewayRequestSpec{Route: "/v1/audio/transcriptions"}, wav)
	require.Equal(t, int64(24000), m.Numerator)
	require.Equal(t, int64(16000), m.Denominator)
	binary.LittleEndian.PutUint32(wav[28:], 1000000)
	m = requestMediaMeter(service.GatewayRequestSpec{Route: "/v1/audio/transcriptions"}, wav)
	require.Zero(t, m.Denominator)
}
