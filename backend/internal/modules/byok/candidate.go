package byok

import (
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/routing/snapshot"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/policy"
	"slices"
)

// BindCandidate maps an Eligible credential into the planner graph. Native
// entitlement, capacity and rejection facts are supplied by the candidate
// producer. Funding consent can only narrow the producer's authorization.
func BindCandidate(credential Credential, account int64, consent bool, candidate snapshot.Candidate, fact policy.Evidence) (snapshot.Candidate, policy.Evidence, error) {
	if credential.State != "verified" || account <= 0 || !slices.Contains(credential.Scope.Models, candidate.ModelID) {
		return candidate, fact, fault.Forbidden
	}
	switch candidate.FundingSource {
	case "byok":
		if account != credential.ID || fact.Provider != credential.Provider || fact.Connection != nil {
			return candidate, fact, fault.Forbidden
		}
		fact.BYOKToPlatform = false
		candidate.Tier = credential.Priority
	case "wallet":
		if account == credential.ID || fact.Connection == nil || fact.Connection.AccountID != account {
			return candidate, fact, fault.Forbidden
		}
		fact.BYOKToPlatform = true
		candidate.EndpointFallback = true
		candidate.FundingAuthorized = candidate.FundingAuthorized && credential.Fallback && consent
		candidate.Tier = 1001
	default:
		return candidate, fact, fault.Forbidden
	}
	candidate.ID = CandidateID(credential, account, candidate.FundingSource)
	return candidate, fact, nil
}
