package byok

import (
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/routing/planner"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/routing/snapshot"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/routing/trace"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/policy"
	"github.com/stretchr/testify/require"
)

func TestCandidateOrderAndFundingGates(t *testing.T) {
	c := Credential{ID: 10, Workspace: 1, Provider: "openai", Version: 2, State: "verified", Scope: Scope{Models: []string{"m"}}, Priority: 2}
	base := snapshot.Candidate{ModelID: "m", FundingSource: "byok", FundingAuthorized: true}
	facts := policy.Evidence{Provider: "openai"}
	primary, _, e := BindCandidate(c, c.ID, false, base, facts)
	require.NoError(t, e)
	backupCredential := c
	backupCredential.ID++
	backupCredential.Priority = 9
	backup, _, e := BindCandidate(backupCredential, backupCredential.ID, false, base, facts)
	require.NoError(t, e)
	backup.Score = 100 // Native scoring only breaks ties within a preference tier.
	base.FundingSource = "wallet"
	facts.Connection = &policy.ConnectionFence{AccountID: 55}
	for _, v := range []struct{ grant, consent, platform bool }{{false, true, true}, {true, false, true}, {true, true, false}, {true, true, true}} {
		c.Fallback = v.grant
		base.FundingAuthorized = v.platform
		platform, f, e := BindCandidate(c, 55, v.consent, base, facts)
		require.NoError(t, e)
		require.True(t, f.BYOKToPlatform)
		require.True(t, platform.EndpointFallback)
		require.Equal(t, v.grant && v.consent && v.platform, platform.FundingAuthorized)
		result := planner.Plan(planner.Features{Mode: trace.Execution, ModelID: "m"}, planner.EffectivePolicy{AllowEndpointFallback: true, RequireAuthorizedFunding: true, IncludeOverflow: true}, snapshot.CandidateSnapshot{AsOf: time.Now(), Candidates: []snapshot.Candidate{platform, backup, primary}})
		require.Equal(t, primary.ID, result.Selected.CandidateID)
		require.Equal(t, backup.ID, result.Candidates[1].ID)
		if platform.FundingAuthorized {
			require.Equal(t, "eligible", result.Candidates[2].State)
		} else {
			require.Equal(t, "excluded", result.Candidates[2].State)
		}
	}
	_, _, e = BindCandidate(c, c.ID, true, base, facts)
	require.Error(t, e)
	wrong := c
	wrong.Version++
	require.NotEqual(t, CandidateID(c, 55, "wallet"), CandidateID(wrong, 55, "wallet"))
}
