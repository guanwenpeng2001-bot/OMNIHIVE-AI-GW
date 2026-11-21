package verification

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOpenAIChatCompatibilityFixture(t *testing.T) {
	data, err := os.ReadFile("../../../../../contracts/api/catalog-verification/compatibility-example.json")
	require.NoError(t, err)
	var sample struct {
		ID      string `json:"fixture_id"`
		Version string `json:"adapter_version"`
		Request struct {
			Body json.RawMessage
		}
	}
	require.NoError(t, json.Unmarshal(data, &sample))
	require.Equal(t, OpenAIChatFixtureID, sample.ID)
	require.Equal(t, OpenAIChatAdapterVersion, sample.Version)
	require.JSONEq(t, openAIChatExample, string(sample.Request.Body))
	require.True(t, checkOpenAIChatFixture())
}

func TestObservationFreshnessAndSample(t *testing.T) {
	at := time.Unix(1700000000, 0).UTC()
	obs := catalog.CompatibilityEvidence{ObservedAt: at, ExpiresAt: at.Add(time.Hour)}
	for _, test := range []struct {
		name      string
		probe     service.ConnectionProbe
		state     string
		freshness string
		samples   int
	}{
		{"successful", service.ConnectionProbe{Network: "passed", Authorization: "passed", Discovery: "passed", Models: []string{"exact"}}, success, "fresh", 1},
		{"rejected", service.ConnectionProbe{Network: "passed", Authorization: "failed"}, failed, "fresh", 1},
		{"public catalog", service.ConnectionProbe{Network: "passed", Authorization: "unknown", Discovery: "passed", Models: []string{"exact"}}, unknown, "unknown", 0},
		{"unlisted model", service.ConnectionProbe{Network: "passed", Authorization: "passed", Discovery: "passed", Models: []string{"other"}}, unknown, "unknown", 0},
		{"missing network", service.ConnectionProbe{Network: "failed"}, unknown, "unknown", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := classifyObservation(test.probe, "exact")
			require.Equal(t, test.state, result.state)
			require.Equal(t, test.samples, result.samples)
			obs.State, obs.SampleCount = result.state, result.samples
			require.Equal(t, test.freshness, obs.At(at).Freshness)
			require.Equal(t, "stale", obs.At(obs.ExpiresAt).Freshness)
			if test.state != success {
				require.Nil(t, obs.At(at).VerifiedAt)
			}
		})
	}
}
