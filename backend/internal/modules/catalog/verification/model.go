package verification

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
)

const (
	VerifyGrant  = "catalog.verify"
	PublishGrant = "catalog.publish"

	candidate    = "candidate"
	experimental = "experimental"
	stable       = "stable"
	deprecated   = "deprecated"
	closed       = "closed"

	success = "success"
	failed  = "failed"
	unknown = "unknown"

	liveOrigin       = "endpoint_probe"
	connectionOrigin = "connection_verification"
)

var (
	queueFull         = &fault.Fault{Status: 429, Reason: "VERIFICATION_QUEUE_FULL", Message: "The verification queue is full. Try again later."}
	inProgress        = &fault.Fault{Status: 409, Reason: "VERIFICATION_IN_PROGRESS", Message: "This target already has a pending verification."}
	targetExists      = &fault.Fault{Status: 409, Reason: "VERIFICATION_TARGET_EXISTS", Message: "A target already exists for this adapter version and matrix tuple."}
	badTransition     = &fault.Fault{Status: 409, Reason: "COMPATIBILITY_TRANSITION_CONFLICT", Message: "This maturity change is outside the compatibility lifecycle."}
	evidenceRequired  = &fault.Fault{Status: 409, Reason: "CURRENT_FIXTURE_EVIDENCE_REQUIRED", Message: "Use a current successful probe with matching compatibility fixture evidence."}
	targetUnavailable = &fault.Fault{Status: 409, Reason: "VERIFICATION_TARGET_UNAVAILABLE", Message: "Select an active platform connection with matching provider and account mode."}
)

type Matrix struct {
	EntryID        string `json:"entry_id"`
	AdapterVersion string `json:"adapter_version"`
	Action         string `json:"action"`
	AccountMode    string `json:"account_mode"`
	Protocol       string `json:"protocol"`
}

func (m Matrix) cellID() string {
	return catalog.Digest([4]string{m.EntryID, m.Action, m.AccountMode, m.Protocol})
}

func (m Matrix) validate(optionalProtocol bool) error {
	switch {
	case !entryIDPattern.MatchString(m.EntryID):
		return fault.Invalid("/entry_id")
	case !labelPattern.MatchString(m.AdapterVersion):
		return fault.Invalid("/adapter_version")
	case !validMode(m.AccountMode):
		return fault.Invalid("/account_mode")
	case optionalProtocol && m.Protocol == "":
		if !slices.ContainsFunc(methods, func(method string) bool {
			action, _, _ := catalog.GatewayAction(method)
			return action == m.Action
		}) {
			return fault.Invalid("/action")
		}
	case adapterAction(m.Action, m.Protocol) == "":
		return fault.Invalid("/protocol")
	}
	return nil
}

var (
	entryIDPattern = regexp.MustCompile(`^m_[a-f0-9]{32}$`)
	labelPattern   = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,63}$`)
	methods        = []string{"chat_completions", "responses", "messages", "gemini", "count_tokens", "embeddings",
		"images", "images/edits", "rerank", "speech", "transcriptions", "live", "alpha_search", "grok_media_generation"}
)

func adapterAction(action, protocol string) string {
	for _, method := range methods {
		a, p, adapter := catalog.GatewayAction(method)
		if action == a && protocol == p {
			return adapter
		}
	}
	return ""
}

func validMode(mode string) bool {
	switch mode {
	case "api_key", domain.AccountTypeOAuth, domain.AccountTypeSetupToken, domain.AccountTypeServiceAccount,
		domain.AccountTypeBedrock, domain.AccountModePayG, domain.AccountModeCoding:
		return true
	}
	return false
}

type CreateTarget struct {
	Provider        string `json:"provider"`
	Model           string `json:"model"`
	ConnectionID    int64  `json:"connection_id,string"`
	AdapterVersion  string `json:"adapter_version"`
	Action          string `json:"action"`
	AccountMode     string `json:"account_mode"`
	Protocol        string `json:"protocol"`
	IntervalSeconds int    `json:"interval_seconds"`
}

func (in CreateTarget) matrix() Matrix {
	return Matrix{
		EntryID: catalog.StableID(in.Provider, in.Model), AdapterVersion: in.AdapterVersion,
		Action: in.Action, AccountMode: in.AccountMode, Protocol: in.Protocol,
	}
}

func (in CreateTarget) validate() error {
	if _, known := domain.LookupPlatform(in.Provider); !known {
		return fault.Invalid("/provider")
	}
	if in.Model == "" || len(in.Model) > 256 || strings.IndexFunc(in.Model, unicode.IsSpace) >= 0 ||
		strings.IndexFunc(in.Model, unicode.IsControl) >= 0 || strings.ContainsAny(in.Model, "?#") {
		return fault.Invalid("/model")
	}
	if in.ConnectionID <= 0 {
		return fault.Invalid("/connection_id")
	}
	if err := validateInterval(in.IntervalSeconds); err != nil {
		return err
	}
	return in.matrix().validate(false)
}

func validateInterval(interval int) error {
	if interval != 0 && (interval < 300 || interval > 604800) {
		return fault.Invalid("/interval_seconds")
	}
	return nil
}

type Target struct {
	ID string `json:"id"`
	Matrix
	Provider        string     `json:"provider"`
	Model           string     `json:"model"`
	ConnectionID    int64      `json:"connection_id,string"`
	OwnerID         int64      `json:"owner_id,string"`
	IntervalSeconds int        `json:"interval_seconds"`
	ScheduleVersion int64      `json:"schedule_version,string"`
	NextDueAt       *time.Time `json:"next_due_at"`
	CreatedAt       time.Time  `json:"created_at"`
}

type TargetDetail struct {
	Target
	LatestAttempt *Observation `json:"latest_attempt"`
	LastSuccess   *Observation `json:"last_success"`
	Publication   *Publication `json:"publication"`
}

type Observation struct {
	catalog.CompatibilityEvidence
	Matrix Matrix `json:"matrix"`
	Reason string `json:"reason"`
}

type Publish struct {
	Maturity        string     `json:"maturity,omitempty"`
	EvidenceID      string     `json:"evidence_id,omitempty"`
	Reason          string     `json:"deprecation_reason,omitempty"`
	SunsetAt        *time.Time `json:"sunset_at,omitempty"`
	RollbackVersion *int64     `json:"rollback_version,string,omitempty"`
}

type Publication struct {
	catalog.Compatibility
	TargetID        string `json:"target_id"`
	EntryID         string `json:"entry_id"`
	RollbackVersion *int64 `json:"rollback_version,string"`
}

func authorize(ctx context.Context, access identity.AccessEvaluator, actor int64, grant string) error {
	if actor <= 0 {
		return fault.Unauthenticated
	}
	scope, err := access.Evaluate(ctx, actor, domain.DefaultWorkspaceID, 0, grant)
	if err != nil {
		return err
	}
	if scope.ActorID != actor || scope.WorkspaceID != domain.DefaultWorkspaceID || scope.ProjectID != 0 {
		return fault.Forbidden
	}
	return scope.Require(grant)
}

func prefixedID(id, prefix string) (uuid.UUID, error) {
	raw, ok := strings.CutPrefix(id, prefix)
	value, err := uuid.Parse(raw)
	if !ok || err != nil || value == uuid.Nil || raw != value.String() {
		return uuid.Nil, fault.NotFound
	}
	return value, nil
}

func denied(err error) bool {
	return errors.Is(err, fault.Forbidden) || errors.Is(err, fault.NotFound) || errors.Is(err, fault.Unauthenticated)
}
