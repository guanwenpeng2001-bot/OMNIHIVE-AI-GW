package service

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/journal"
	grants "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/extensions/actions"
	v2 "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/pkg/pluginapi/v2"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/pkg/pluginapi/v2/actions"
)

// Leave WASI capacity available for BE-20 operations when optional planning is busy.
var typedActionSlots = make(chan struct{}, 2)

type actionAdmission struct {
	runtime *pluginRuntime
	grant   grants.Grant
}
type actionAdmissionKey struct{}

func isTypedAction(cap string) bool {
	return cap == actions.ProviderResponses || cap == actions.TaskImageGenerate
}

func (m *PluginManager) actionRuntime(cap, platform, accountType string) *pluginRuntime {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, registry := range []map[int64]*pluginRuntime{m.auxiliary, m.runtimes} {
		for _, r := range registry {
			if r.extension == nil || r.draining.Load() || r.exited() {
				continue
			}
			for _, c := range r.extension.manifest.Capabilities {
				if c.ID == cap && c.Platform == platform && c.AccountType == accountType {
					return r
				}
			}
		}
	}
	return nil
}

func actionKind(spec GatewayRequestSpec, path string) actions.Kind {
	switch {
	case spec.ExecutionKind == "request" && spec.Protocol == "responses" && strings.HasSuffix(path, "/responses"):
		return actions.Kind_PROVIDER_RESPONSES
	case spec.ExecutionKind == "job" && spec.Protocol == "images" && (strings.HasSuffix(path, "/images/generations") || strings.HasSuffix(path, "/responses")):
		return actions.Kind_TASK_IMAGE_GENERATE
	default:
		return actions.Kind_UNSPECIFIED
	}
}

// Only the existing admitted native send can run. Failure of the optional
// planner discards its plan. No guest response authorizes a second send.
func (m *PluginManager) actionTransfer(req *http.Request, account *Account) (actions.Transfer, bool) {
	ctx := req.Context()
	plan := GatewayRequestPlanFromContext(ctx)
	if req.Method != http.MethodPost || plan == nil || account == nil || account.ParentAccountID != nil {
		return 0, false
	}
	kind := actionKind(plan.spec, strings.TrimRight(req.URL.Path, "/"))
	cap := kind.Capability()
	if cap == "" {
		return 0, false
	}
	r := m.actionRuntime(cap, account.Platform, account.Type)
	if r == nil {
		return 0, false
	}
	frozen, ok := scope.From(ctx)
	ex, err := plan.AdmittedExecution()
	if err != nil || !ok || ex.RequestID == "" || frozen != ex.Scope || !plan.BudgetAdmitted() ||
		frozen.WorkspaceID != account.WorkspaceID || frozen.ProjectID <= 0 || frozen.ActorID <= 0 || frozen.KeyID <= 0 {
		return 0, false
	}
	version := CredentialVersion(account.Credentials)
	reference, referenced := CredentialReference(account.Credentials)
	if !referenced || reference == "" || version <= 0 {
		return 0, false
	}
	target := grants.Target{Workspace: frozen.WorkspaceID, Project: frozen.ProjectID, Account: account.ID,
		CredentialVersion: version, CredentialReference: reference, Model: plan.spec.Model, Capability: cap}
	// Bound both WASI exchanges and all ancillary authorization reads together.
	timeout := time.Second
	if deadline, ok := ctx.Deadline(); ok {
		timeout = min(timeout, time.Until(deadline)/4)
	}
	child, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	reader, ok := m.repo.(grants.Reader)
	if !ok {
		return 0, false
	}
	outcome := "denied"
	defer func() {
		if errors.Is(child.Err(), context.DeadlineExceeded) {
			outcome = "timeout"
		}
		slog.Info("extension_action", "actor_id", frozen.ActorID, "workspace_id", frozen.WorkspaceID, "project_id", frozen.ProjectID,
			"account_id", account.ID, "plugin_id", r.installation.ID, "action", cap, "outcome", outcome)
	}()
	select {
	case typedActionSlots <- struct{}{}:
		defer func() { <-typedActionSlots }()
	default:
		outcome = "busy"
		return 0, false
	}
	g, err := reader.LookupExtensionAction(child, target)
	if errors.Is(err, sql.ErrNoRows) {
		outcome = "ungranted"
		return 0, false
	}
	if err != nil || !grants.InRollout(account.ID, g.Rollout) || g.PluginID != r.installation.ID ||
		g.Version != r.extension.manifest.Version || g.ArchiveDigest != r.extension.archiveDigest {
		return 0, false
	}
	if err := journal.Authorize(child, frozen, plan.spec.Model); err != nil {
		return 0, false
	}
	child = context.WithValue(child, actionAdmissionKey{}, actionAdmission{r, g})
	if err := m.AuthorizeExtension(child, g.PluginID, cap); err != nil {
		return actions.Transfer_HOST_DEFAULT, true
	}
	if !r.beginRequest() {
		return actions.Transfer_HOST_DEFAULT, true
	}
	defer r.finishRequest()
	reply, err := r.extension.invoke(child, &v2.Request{Operation: "dispatch", Capability: cap,
		Action: &actions.Request{Kind: kind}})
	if err != nil {
		outcome = "failed"
		return actions.Transfer_HOST_DEFAULT, true
	}
	result := reply.Action
	if !result.ValidFor(kind) || len(reply.NormalizedConfigJson) != 0 {
		outcome = "invalid_plan"
		return actions.Transfer_HOST_DEFAULT, true
	}
	current, err := reader.LookupExtensionAction(child, target)
	if err != nil || current != g || r.draining.Load() || r.extension.trusted() != nil || child.Err() != nil {
		return actions.Transfer_HOST_DEFAULT, true
	}
	outcome = "applied"
	return result.Transfer, true
}

// The native send rechecks scope and consumes its generation permit after the
// optional guest returns, including on timeout and revocation.
func (m *PluginManager) forwardAction(r *http.Request, account *Account, forward func(*http.Request) (*http.Response, error)) (*http.Response, error) {
	if m == nil {
		return forward(r)
	}
	transfer, selected := m.actionTransfer(r, account)
	if !selected {
		return forward(r)
	}
	if transfer == actions.Transfer_IDENTITY && r.Header.Get("Accept-Encoding") == "" {
		r = r.Clone(r.Context())
		r.Header.Set("Accept-Encoding", "identity")
	}
	return controlledProvider(r, forward)
}
