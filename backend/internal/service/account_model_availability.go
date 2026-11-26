package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

const (
	ModelAvailabilityExtraKey  = "model_availability"
	ModelProbeBudgetExtraKey   = "model_probe_budget"
	ModelEvidenceAdministrator = "administrator_declaration"
	ModelEvidenceProbe         = "probe_success"
)

var (
	ErrModelAvailabilityInvalid  = errors.New("invalid model availability declaration")
	ErrModelAvailabilityNotFound = errors.New("model availability declaration not found")
	ErrModelProbeBudgetExceeded  = errors.New("model probe budget exhausted")
	ErrModelProbeAttemptClaimed  = errors.New("model probe attempt already claimed or expired")
)

// Availability is independent of capability defaults and native discovery.
type ModelAvailabilityDeclaration struct {
	AccountID                  int64      `json:"account_id"`
	Model                      string     `json:"model"`
	Endpoint                   string     `json:"endpoint"`
	IdentityRevision           int64      `json:"identity_revision"`
	RulesRevision              int64      `json:"rules_revision"`
	CredentialAccountID        int64      `json:"credential_account_id,omitempty"`
	CredentialIdentityRevision int64      `json:"credential_identity_revision,omitempty"`
	Reason                     string     `json:"reason"`
	ConfirmedBy                int64      `json:"confirmed_by"`
	ConfirmedAt                time.Time  `json:"confirmed_at"`
	ExpiresAt                  *time.Time `json:"expires_at,omitempty"`
	RevokedAt                  *time.Time `json:"revoked_at,omitempty"`
	EvidenceKind               string     `json:"evidence_kind"`
}

type ModelAvailabilityChange struct {
	AccountModelCatalogBaseline
	Model     string     `json:"model"`
	Endpoint  string     `json:"endpoint"`
	Reason    string     `json:"reason"`
	Confirmed bool       `json:"confirmed"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type ModelAvailabilityWriter interface {
	ChangeModelAvailability(context.Context, int64, ModelAvailabilityChange, int64, bool) error
}

func (account *Account) ModelAvailabilityDeclarations() []ModelAvailabilityDeclaration {
	declarations := []ModelAvailabilityDeclaration{}
	if account != nil {
		if !decodeCatalogExtra(account.Extra, ModelAvailabilityExtraKey, &declarations) {
			return []ModelAvailabilityDeclaration{}
		}
	}
	if declarations == nil {
		return []ModelAvailabilityDeclaration{}
	}
	return declarations
}

func concreteMappedModel(account *Account, model string) bool {
	return exactConfiguredModel(model) && slices.Contains(configuredConcreteModelIDs(account), model)
}

func ModelAvailabilityEndpointValid(endpoint string) bool {
	switch endpoint {
	case "responses", "chat_completions", "messages", "generate_content", "embeddings", "images", "videos", "grok_media_generation", "audio_speech", "audio_transcriptions", "audio_translations", "rerank", "realtime", "live", "alpha_search":
		return true
	default:
		return false
	}
}

func (declaration ModelAvailabilityDeclaration) ValidFor(account, credential *Account, now time.Time) bool {
	if account == nil || declaration.AccountID != account.ID || declaration.RevokedAt != nil || declaration.ConfirmedAt.IsZero() || declaration.ConfirmedAt.After(now) || !concreteMappedModel(account, declaration.Model) || !ModelAvailabilityEndpointValid(declaration.Endpoint) {
		return false
	}
	state := account.ModelCatalogState()
	if state.Result == "credentials_invalid" || declaration.IdentityRevision != state.IdentityRevision {
		return false
	}
	if declaration.ExpiresAt != nil && !now.Before(*declaration.ExpiresAt) {
		return false
	}
	if declaration.EvidenceKind != ModelEvidenceAdministrator && declaration.EvidenceKind != ModelEvidenceProbe {
		return false
	}
	if declaration.EvidenceKind == ModelEvidenceAdministrator && (declaration.ConfirmedBy <= 0 || strings.TrimSpace(declaration.Reason) == "") {
		return false
	}
	if declaration.EvidenceKind == ModelEvidenceProbe && (declaration.ExpiresAt == nil || declaration.ExpiresAt.After(declaration.ConfirmedAt.AddDate(0, 1, 0))) {
		return false
	}
	if account.IsShadow() || declaration.CredentialAccountID != 0 {
		if !account.IsShadow() || credential == nil || credential.ID != *account.ParentAccountID || credential.ID != declaration.CredentialAccountID || !credential.IsOpenAIOAuth() || credential.IsShadow() || !credential.IsCredentialUsableForShadow() || credential.ModelCatalogState().Result == "credentials_invalid" || credential.ModelCatalogState().IdentityRevision != declaration.CredentialIdentityRevision {
			return false
		}
	}
	return true
}

func ValidateModelAvailabilityChange(account *Account, change ModelAvailabilityChange, actor int64, revoke bool, now time.Time) error {
	if account == nil || actor <= 0 || strings.TrimSpace(change.Reason) == "" || len(change.Reason) > 2000 || len(change.Model) > 512 || !ModelAvailabilityEndpointValid(change.Endpoint) {
		return ErrModelAvailabilityInvalid
	}
	baseline := account.ModelCatalogBaseline()
	if baseline.IdentityRevision != change.IdentityRevision || baseline.RulesRevision != change.RulesRevision || baseline.SnapshotRevision != change.SnapshotRevision {
		return ErrAccountModelCatalogConflict
	}
	if !revoke && (!change.Confirmed || !concreteMappedModel(account, change.Model) || (change.ExpiresAt != nil && !now.Before(*change.ExpiresAt))) {
		return ErrModelAvailabilityInvalid
	}
	return nil
}

func (service *AccountTestService) ChangeModelAvailability(ctx context.Context, account *Account, change ModelAvailabilityChange, actor int64, revoke bool) error {
	if err := ValidateModelAvailabilityChange(account, change, actor, revoke, time.Now().UTC()); err != nil {
		return err
	}
	writer, ok := service.accountRepo.(ModelAvailabilityWriter)
	if !ok {
		return fmt.Errorf("model availability persistence unavailable")
	}
	return writer.ChangeModelAvailability(ctx, account.ID, change, actor, revoke)
}

// appendAvailabilityModels is a read projection. It never changes discovered or
// persists invented native evidence, including on platforms without discovery.
func appendAvailabilityModels(account, credential *Account, snapshot *AccountModelCatalogSnapshot) *AccountModelCatalogSnapshot {
	for _, declaration := range account.ModelAvailabilityDeclarations() {
		if !declaration.ValidFor(account, credential, time.Now().UTC()) {
			continue
		}
		if snapshot == nil {
			snapshot = &AccountModelCatalogSnapshot{IdentityRevision: account.ModelCatalogState().IdentityRevision, EligibleModels: []AccountCatalogModel{}}
		}
		snapshot.EligibleModels = append(snapshot.EligibleModels, AccountCatalogModel{ID: declaration.Model, Endpoints: []string{declaration.Endpoint}, EvidenceKind: declaration.EvidenceKind, EvidenceObservedAt: declaration.ConfirmedAt.Format(time.RFC3339Nano)})
	}
	return snapshot
}

func hasAvailabilityEvidence(account, credential *Account, member AccountCatalogModel) bool {
	for _, declaration := range account.ModelAvailabilityDeclarations() {
		if declaration.Model == member.ID && declaration.EvidenceKind == member.EvidenceKind && len(member.Endpoints) == 1 && member.Endpoints[0] == declaration.Endpoint && declaration.ConfirmedAt.Format(time.RFC3339Nano) == member.EvidenceObservedAt && declaration.ValidFor(account, credential, time.Now().UTC()) {
			return true
		}
	}
	return false
}

// The repository serializes this state under the account row lock. Revisions
// are independent of configuration CAS; token changes never refund attempts.
type ModelProbeBudget struct {
	Revision         int64               `json:"revision"`
	Day              string              `json:"day"`
	Month            string              `json:"month"`
	DailyUsed        int                 `json:"daily_used"`
	MonthlyUsed      int                 `json:"monthly_used"`
	MediaDailyUsed   int                 `json:"media_daily_used"`
	MediaMonthlyUsed int                 `json:"media_monthly_used"`
	Attempts         []ModelProbeAttempt `json:"attempts"`
}

type ModelProbeAttempt struct {
	ID                         string     `json:"id"`
	AccountID                  int64      `json:"account_id"`
	Model                      string     `json:"model"`
	Endpoint                   string     `json:"endpoint"`
	IdentityRevision           int64      `json:"identity_revision"`
	RulesRevision              int64      `json:"rules_revision"`
	CredentialAccountID        int64      `json:"credential_account_id,omitempty"`
	CredentialIdentityRevision int64      `json:"credential_identity_revision,omitempty"`
	ReservedAt                 time.Time  `json:"reserved_at"`
	ClaimedAt                  *time.Time `json:"claimed_at,omitempty"`
	CompletedAt                *time.Time `json:"completed_at,omitempty"`
	Outcome                    string     `json:"outcome,omitempty"`
}

type ModelProbeOutcome string

const (
	ModelProbeSucceeded ModelProbeOutcome = "success"
	ModelProbeMissing   ModelProbeOutcome = "model_missing"
	ModelProbeTemporary ModelProbeOutcome = "temporarily_unavailable"
)

// An adapter must attest to the correct target, valid request and explicit
// missing-model error; a generic 400/404 is never evidence of model absence.
func ClassifyModelProbeOutcome(status int, validTargetRequest, explicitModelMissing, finalSuccess bool) ModelProbeOutcome {
	if validTargetRequest && finalSuccess && status >= 200 && status < 300 {
		return ModelProbeSucceeded
	}
	if validTargetRequest && explicitModelMissing && (status == 400 || status == 404) {
		return ModelProbeMissing
	}
	return ModelProbeTemporary
}

type ModelProbeBudgetRepository interface {
	ReserveModelProbe(context.Context, int64, AccountModelCatalogBaseline, string, string) (*ModelProbeAttempt, error)
	ClaimModelProbe(context.Context, int64, string) (*ModelProbeAttempt, error)
	CompleteModelProbe(context.Context, int64, string, ModelProbeOutcome) error
}

func ModelProbeIsMedia(endpoint string) bool {
	return endpoint == "images" || endpoint == "videos" || endpoint == "grok_media_generation"
}

// Reserve runs only with database time and the current locked state. All
// outstanding attempts expire at the UTC month boundary, never their counters.
func (budget *ModelProbeBudget) Reserve(attempt ModelProbeAttempt) error {
	now := attempt.ReservedAt.UTC()
	day, month := now.Format("2006-01-02"), now.Format("2006-01")
	if budget.Month > month || budget.Day > day {
		return ErrModelProbeBudgetExceeded
	}
	if budget.Month != month {
		budget.Month, budget.MonthlyUsed, budget.MediaMonthlyUsed = month, 0, 0
		budget.Attempts = nil
	}
	if budget.Day != day {
		budget.Day, budget.DailyUsed, budget.MediaDailyUsed = day, 0, 0
	}
	media := ModelProbeIsMedia(attempt.Endpoint)
	if budget.DailyUsed >= 5 || budget.MonthlyUsed >= 50 || (media && (budget.MediaDailyUsed >= 1 || budget.MediaMonthlyUsed >= 8)) {
		return ErrModelProbeBudgetExceeded
	}
	budget.DailyUsed++
	budget.MonthlyUsed++
	if media {
		budget.MediaDailyUsed++
		budget.MediaMonthlyUsed++
	}
	budget.Revision++
	budget.Attempts = append(budget.Attempts, attempt)
	return nil
}
