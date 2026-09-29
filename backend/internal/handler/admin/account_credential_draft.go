package admin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"net/http"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const credentialDraftBrowserCookie = "account_authorization_browser"

// IssueCursorCredentialDraft validates manually supplied Cursor credentials
// without creating an account or returning the credentials to the browser.
func (handler *AccountHandler) IssueCursorCredentialDraft(ctx *gin.Context) {
	var request struct {
		Credentials map[string]any `json:"credentials" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&request); err != nil {
		response.BadRequest(ctx, "Invalid Cursor credentials")
		return
	}
	for key := range request.Credentials {
		switch key {
		case "access_token", "refresh_token", "machine_id", "mac_machine_id", "client_version", "model_mapping", "compact_model_mapping":
		default:
			response.ErrorFrom(ctx, infraerrors.BadRequest("INVALID_CURSOR_CREDENTIALS", "Unsupported Cursor authorization field: "+key))
			return
		}
	}
	account := &service.Account{Platform: service.PlatformCursor, Type: service.AccountTypeOAuth, Concurrency: 1, Credentials: request.Credentials}
	if err := service.NormalizeCursorAccount(account); err != nil {
		response.ErrorFrom(ctx, infraerrors.BadRequest("INVALID_CURSOR_CREDENTIALS", err.Error()))
		return
	}
	respondCredentialDraft(ctx, handler.credentialDrafts, account.Platform, account.Type, account.Credentials)
}

func accountWriteError(err error) error {
	var mixed *service.MixedChannelError
	if errors.As(err, &mixed) {
		return infraerrors.Conflict("mixed_channel_warning", mixed.Error()).WithMetadata(map[string]string{
			"group_id":         strconv.FormatInt(mixed.GroupID, 10),
			"group_name":       mixed.GroupName,
			"current_platform": mixed.CurrentPlatform,
			"other_platform":   mixed.OtherPlatform,
		})
	}
	return err
}

func claudeDraftCredentials(token *service.TokenInfo) map[string]any {
	return map[string]any{
		"access_token": token.AccessToken, "refresh_token": token.RefreshToken,
		"expires_at": strconv.FormatInt(token.ExpiresAt, 10), "token_type": token.TokenType,
		"scope": token.Scope, "org_uuid": token.OrgUUID, "account_uuid": token.AccountUUID,
		"email_address": token.EmailAddress,
	}
}

type credentialDraftHandler struct {
	credentialDrafts *service.AuthPendingIdentityService
}

func (handler *credentialDraftHandler) SetCredentialDraftService(pending *service.AuthPendingIdentityService) {
	handler.credentialDrafts = pending
}

func requireCredentialDraftStore(pending *service.AuthPendingIdentityService) error {
	if pending == nil {
		return infraerrors.ServiceUnavailable("CREDENTIAL_DRAFT_NOT_READY", "Credential draft store is unavailable")
	}
	return nil
}

func credentialDraftBinding(ctx *gin.Context, initialize bool) (string, error) {
	browser, err := ctx.Cookie(credentialDraftBrowserCookie)
	if err != nil || len(browser) != 64 {
		if !initialize {
			return "", service.ErrPendingAuthBrowserMismatch
		}
		entropy := make([]byte, 32)
		if _, err := rand.Read(entropy); err != nil {
			return "", err
		}
		browser = hex.EncodeToString(entropy)
		ctx.SetSameSite(http.SameSiteStrictMode)
		cookiePath := "/api/v1/admin"
		if workspaceID, scoped := domain.WorkspaceScope(ctx.Request.Context()); scoped {
			cookiePath = "/api/v1/workspaces/" + strconv.FormatInt(workspaceID, 10)
		}
		ctx.SetCookie(credentialDraftBrowserCookie, browser, 0, cookiePath, "", ctx.Request.TLS != nil || strings.EqualFold(ctx.GetHeader("X-Forwarded-Proto"), "https"), true)
	}
	// The ambient bearer credential remains the authorization boundary. The
	// HttpOnly random browser proof additionally prevents reference copying.
	digest := sha256.Sum256([]byte(adminActorScope(ctx) + ":" + browser))
	return hex.EncodeToString(digest[:]), nil
}

func beginCredentialAuthorization(ctx *gin.Context, pending *service.AuthPendingIdentityService, purpose, sessionID string) (string, error) {
	binding, err := credentialDraftBinding(ctx, true)
	if err != nil {
		return "", err
	}
	err = requireCredentialDraftStore(pending)
	if err != nil {
		return "", err
	}
	return pending.IssueIdentityTicket(ctx.Request.Context(), "account-authorization:"+purpose, binding, map[string]any{"authorization_session": sessionID}, 10*time.Minute)
}

func consumeCredentialAuthorization(ctx *gin.Context, pending *service.AuthPendingIdentityService, purpose, reference string) (string, error) {
	binding, err := credentialDraftBinding(ctx, false)
	if err != nil {
		return "", err
	}
	err = requireCredentialDraftStore(pending)
	if err != nil {
		return "", err
	}
	session, err := pending.ConsumeIdentityTicket(ctx.Request.Context(), reference, "account-authorization:"+purpose, binding)
	if err != nil {
		return "", err
	}
	sessionID, ok := session.LocalFlowState["authorization_session"].(string)
	if !ok || sessionID == "" {
		return "", service.ErrPendingAuthCodeInvalid
	}
	return sessionID, nil
}

func respondCredentialDraft(ctx *gin.Context, pending *service.AuthPendingIdentityService, platform, accountType string, credentials map[string]any) {
	binding, err := credentialDraftBinding(ctx, true)
	if err != nil {
		response.ErrorFrom(ctx, err)
		return
	}
	err = requireCredentialDraftStore(pending)
	if err != nil {
		response.ErrorFrom(ctx, err)
		return
	}
	draft, err := pending.IssueAccountCredentialDraft(ctx.Request.Context(), binding, platform, accountType, credentials)
	if err != nil {
		response.ErrorFrom(ctx, err)
		return
	}
	response.Success(ctx, draft)
}

func (handler *OAuthHandler) PreviewCredentialDraft(ctx *gin.Context) {
	binding, err := credentialDraftBinding(ctx, false)
	if err != nil {
		response.ErrorFrom(ctx, err)
		return
	}
	pending := handler.credentialDrafts
	err = requireCredentialDraftStore(pending)
	if err != nil {
		response.ErrorFrom(ctx, err)
		return
	}
	draft, err := pending.PreviewAccountCredentialDraft(ctx.Request.Context(), ctx.Param("draft"), binding)
	if err != nil {
		response.ErrorFrom(ctx, err)
		return
	}
	response.Success(ctx, draft)
}

func (handler *OAuthHandler) CancelCredentialDraft(ctx *gin.Context) {
	binding, err := credentialDraftBinding(ctx, false)
	if err != nil {
		response.ErrorFrom(ctx, err)
		return
	}
	pending := handler.credentialDrafts
	err = requireCredentialDraftStore(pending)
	if err != nil {
		response.ErrorFrom(ctx, err)
		return
	}
	if err := pending.CancelAccountCredentialDraft(ctx.Request.Context(), ctx.Param("draft"), binding); err != nil {
		response.ErrorFrom(ctx, err)
		return
	}
	response.Success(ctx, gin.H{"cancelled": true})
}

func credentialDraftSubmission(request CreateAccountRequest) (string, error) {
	request.CredentialDraft, request.CredentialDraftRevision = "", ""
	encoded, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func (handler *AccountHandler) resolveCredentialDraftPreview(ctx *gin.Context, request SyncUpstreamModelsPreviewRequest, account *service.Account) error {
	if err := requireCredentialDraftStore(handler.credentialDrafts); err != nil {
		return err
	}
	binding, err := credentialDraftBinding(ctx, false)
	if err != nil {
		return err
	}
	draft, revision, err := handler.credentialDrafts.ResolveAccountCredentialDraft(ctx.Request.Context(), request.CredentialDraft, binding)
	if err != nil {
		return err
	}
	if revision != request.CredentialDraftRevision || request.APIKey != "" || request.BaseURL != "" {
		return service.ErrAccountCredentialDraftConflict
	}
	if err := applyCredentialDraftRules(draft, CreateAccountRequest{Platform: request.Platform, Type: request.Type, Credentials: request.Credentials}); err != nil {
		return err
	}
	if request.ModelMapping != nil {
		mapping := make(map[string]any, len(request.ModelMapping))
		for source, target := range request.ModelMapping {
			mapping[source] = target
		}
		draft.Credentials["model_mapping"] = mapping
	}
	account.Credentials = draft.Credentials
	return nil
}

// Only model rules can be overlaid on an authorized identity. Connection,
// authentication and machine identifiers belong to the server-side draft.
func applyCredentialDraftRules(account *service.Account, request CreateAccountRequest) error {
	if account.Platform != request.Platform || account.Type != request.Type {
		return service.ErrAccountCredentialDraftConflict
	}
	for key, value := range request.Credentials {
		switch key {
		case "model_mapping", "compact_model_mapping":
			account.Credentials[key] = value
		default:
			return service.ErrAccountCredentialDraftConflict
		}
	}
	return nil
}

func applyCredentialDraftOptions(account *service.Account, request CreateAccountRequest) error {
	if account.Platform != request.Platform || account.Type != request.Type {
		return service.ErrAccountCredentialDraftConflict
	}
	return service.ApplyAccountCredentialDraftOptions(account, request.Credentials)
}

func accountTypeRequiresCredentialDraft(accountType string) bool {
	return accountType == service.AccountTypeOAuth || accountType == service.AccountTypeSetupToken
}

func errCredentialDraftRequired() error {
	return infraerrors.BadRequest("CREDENTIAL_DRAFT_REQUIRED", "OAuth and setup-token accounts must confirm a credential draft")
}

func cloneAccountRequestExtra(extra map[string]any) map[string]any {
	if extra == nil {
		return nil
	}
	cloned := make(map[string]any, len(extra))
	for key, value := range extra {
		cloned[key] = value
	}
	return cloned
}

func (handler *AccountHandler) createAccountFromRequest(ctx context.Context, ginCtx *gin.Context, request CreateAccountRequest, skipMixedChannelCheck, bindCapabilities bool) (*service.Account, error) {
	extra := cloneAccountRequestExtra(request.Extra)
	if accountTypeRequiresCredentialDraft(request.Type) {
		if strings.TrimSpace(request.CredentialDraft) == "" {
			return nil, errCredentialDraftRequired()
		}
		return handler.confirmCredentialDraftAccount(ctx, ginCtx, request, extra, skipMixedChannelCheck, bindCapabilities)
	}
	if bindCapabilities {
		if err := bindModelCapabilityDefaults(&service.Account{Platform: request.Platform, Type: request.Type, Credentials: service.SanitizeStoredCredentials(request.Platform, request.Credentials)}, extra, adminActorScope(ginCtx)); err != nil {
			return nil, infraerrors.BadRequest("INVALID_MODEL_CAPABILITY_DEFAULTS", err.Error())
		}
	}
	return handler.adminService.CreateAccount(ctx, &service.CreateAccountInput{
		TPMLimit:              request.TPMLimit.Value,
		Name:                  request.Name,
		Notes:                 request.Notes,
		Platform:              request.Platform,
		Type:                  request.Type,
		Credentials:           request.Credentials,
		Extra:                 extra,
		ProxyID:               request.ProxyID,
		Concurrency:           request.Concurrency,
		Priority:              request.Priority,
		RateMultiplier:        request.RateMultiplier,
		LoadFactor:            request.LoadFactor,
		GroupIDs:              request.GroupIDs,
		ExpiresAt:             request.ExpiresAt,
		AutoPauseOnExpired:    request.AutoPauseOnExpired,
		ProbeEnabled:          request.ProbeEnabled,
		SkipMixedChannelCheck: skipMixedChannelCheck,
	})
}

func (handler *AccountHandler) confirmCredentialDraftAccount(ctx context.Context, ginCtx *gin.Context, request CreateAccountRequest, extra map[string]any, skipMixedChannelCheck, bindCapabilities bool) (*service.Account, error) {
	if err := requireCredentialDraftStore(handler.credentialDrafts); err != nil {
		return nil, err
	}
	binding, err := credentialDraftBinding(ginCtx, false)
	if err != nil {
		return nil, err
	}
	return confirmCredentialDraftWithBinding(ctx, handler.credentialDrafts, handler.adminService, binding, request, extra, skipMixedChannelCheck, bindCapabilities, adminActorScope(ginCtx), false)
}

func confirmCredentialDraftWithBinding(
	ctx context.Context,
	pending *service.AuthPendingIdentityService,
	adminService service.AdminService,
	binding string,
	request CreateAccountRequest,
	extra map[string]any,
	skipMixedChannelCheck, bindCapabilities bool,
	actorScope string,
	skipDefaultGroupBind bool,
) (*service.Account, error) {
	if err := requireCredentialDraftStore(pending); err != nil {
		return nil, err
	}
	submission, err := credentialDraftSubmission(request)
	if err != nil {
		return nil, err
	}
	var created *service.Account
	var existing *service.Account
	if request.ExistingAccountID > 0 {
		existing, err = adminService.GetAccount(ctx, request.ExistingAccountID)
		if err != nil {
			return nil, err
		}
		if existing.Platform != request.Platform || existing.Type != request.Type {
			return nil, service.ErrAccountCredentialDraftConflict
		}
	}
	accountID, err := pending.ConfirmAccountCredentialDraft(ctx, request.CredentialDraft, binding, request.CredentialDraftRevision, submission, func(txCtx context.Context, draft *service.Account) (int64, error) {
		if err := applyCredentialDraftOptions(draft, request); err != nil {
			return 0, err
		}
		if bindCapabilities {
			if err := bindModelCapabilityDefaults(draft, extra, actorScope); err != nil {
				return 0, infraerrors.BadRequest("INVALID_MODEL_CAPABILITY_DEFAULTS", err.Error())
			}
		}
		if existing != nil {
			updated, err := pending.UpdateAccountCredentialsIfUnchanged(txCtx, existing, adminService, &service.UpdateAccountInput{
				TPMLimit: request.TPMLimit.Pointer(),
				Name:     request.Name, Notes: request.Notes, Status: request.Status,
				Credentials: draft.Credentials, Extra: extra,
				Concurrency: &request.Concurrency, Priority: &request.Priority,
				RateMultiplier: request.RateMultiplier, LoadFactor: request.LoadFactor,
				ProxyID: request.ProxyID, GroupIDs: &request.GroupIDs,
				ExpiresAt: request.ExpiresAt, AutoPauseOnExpired: request.AutoPauseOnExpired,
				SkipMixedChannelCheck: skipMixedChannelCheck,
			})
			if err != nil {
				return 0, err
			}
			created = updated
			return existing.ID, nil
		}
		account, err := adminService.CreateAccount(txCtx, &service.CreateAccountInput{
			TPMLimit:              request.TPMLimit.Value,
			Name:                  request.Name,
			Notes:                 request.Notes,
			Platform:              draft.Platform,
			Type:                  draft.Type,
			Credentials:           draft.Credentials,
			Extra:                 extra,
			ProxyID:               request.ProxyID,
			Concurrency:           request.Concurrency,
			Priority:              request.Priority,
			RateMultiplier:        request.RateMultiplier,
			LoadFactor:            request.LoadFactor,
			GroupIDs:              request.GroupIDs,
			ExpiresAt:             request.ExpiresAt,
			AutoPauseOnExpired:    request.AutoPauseOnExpired,
			ProbeEnabled:          request.ProbeEnabled,
			SkipDefaultGroupBind:  skipDefaultGroupBind,
			SkipMixedChannelCheck: skipMixedChannelCheck,
		})
		if err != nil {
			return 0, err
		}
		created = account
		return account.ID, nil
	})
	if err != nil {
		return nil, err
	}
	if created != nil && created.ID == accountID {
		service.AfterAccountWriteCommit(ctx, func() {
			if request.ExistingAccountID > 0 {
				// The regular service may read through its base client before the
				// outer batch commits. Publish and probe the committed identity.
				if latest, err := adminService.GetAccount(context.Background(), accountID); err == nil && latest != nil {
					*created = *latest
				}
			}
			adminService.EnsureOpenAIPrivacy(context.Background(), created)
			adminService.EnsureAntigravityPrivacy(context.Background(), created)
			service.InvalidateAvailableModelsCacheForAccount(created)
		})
		return created, nil
	}
	return adminService.GetAccount(ctx, accountID)
}

func (handler *AccountHandler) applyCredentialDraft(ctx context.Context, ginCtx *gin.Context, accountID int64, existing *service.Account, request ApplyOAuthCredentialsRequest) (*service.Account, error) {
	if strings.TrimSpace(request.CredentialDraft) == "" {
		return nil, errCredentialDraftRequired()
	}
	if err := requireCredentialDraftStore(handler.credentialDrafts); err != nil {
		return nil, err
	}
	binding, err := credentialDraftBinding(ginCtx, false)
	if err != nil {
		return nil, err
	}
	review := CreateAccountRequest{
		Name:                    "account:" + strconv.FormatInt(accountID, 10),
		CredentialDraft:         request.CredentialDraft,
		CredentialDraftRevision: request.CredentialDraftRevision,
		Platform:                existing.Platform,
		Type:                    request.Type,
		Credentials:             request.Credentials,
		Extra:                   request.Extra,
	}
	submission, err := credentialDraftSubmission(review)
	if err != nil {
		return nil, err
	}
	confirmedID, err := handler.credentialDrafts.ConfirmAccountCredentialDraft(ctx, request.CredentialDraft, binding, request.CredentialDraftRevision, submission, func(txCtx context.Context, draft *service.Account) (int64, error) {
		if err := applyCredentialDraftOptions(draft, review); err != nil {
			return 0, err
		}
		account, err := handler.credentialDrafts.UpdateAccountCredentialsIfUnchanged(txCtx, existing, handler.adminService, &service.UpdateAccountInput{
			Type:        request.Type,
			Credentials: draft.Credentials,
		})
		if err != nil {
			return 0, err
		}
		if len(request.Extra) > 0 {
			if err := handler.adminService.UpdateAccountExtra(txCtx, accountID, cloneAccountRequestExtra(request.Extra)); err != nil {
				return 0, err
			}
		}
		return account.ID, nil
	})
	if err != nil {
		return nil, err
	}
	if confirmedID != accountID {
		return nil, service.ErrAccountCredentialDraftConflict
	}
	service.InvalidateAvailableModelsCacheForAccount(existing)
	return handler.adminService.GetAccount(ctx, accountID)
}

func (handler *AccountHandler) persistRefreshedCredentials(ctx context.Context, expected *service.Account, credentials map[string]any) (*service.Account, error) {
	if err := requireCredentialDraftStore(handler.credentialDrafts); err != nil {
		return nil, err
	}
	return handler.credentialDrafts.UpdateAccountCredentialsIfUnchanged(ctx, expected, handler.adminService, &service.UpdateAccountInput{
		Credentials: credentials})
}

func (handler *AccountHandler) setCredentialPrivacy(ctx *gin.Context, account *service.Account) (string, error) {
	result, err := executeAdminIdempotent(ctx, "admin.accounts.set-privacy", map[string]any{"account_id": account.ID, "identity": service.AccountRuntimeIdentityKey(account)}, service.DefaultWriteIdempotencyTTL(), func(operationContext context.Context) (any, error) {
		var mode string
		switch account.Platform {
		case service.PlatformOpenAI:
			mode = handler.adminService.ForceOpenAIPrivacy(operationContext, account)
		case service.PlatformAntigravity:
			mode = handler.adminService.ForceAntigravityPrivacy(operationContext, account)
		default:
			return nil, infraerrors.BadRequest("PRIVACY_UNSUPPORTED", "Only OpenAI and Antigravity OAuth accounts support privacy setting")
		}
		if mode == "" {
			return nil, infraerrors.BadRequest("PRIVACY_NOT_APPLIED", "Privacy could not be applied")
		}
		return mode, nil
	})
	if err != nil {
		return "", err
	}
	mode, ok := result.Data.(string)
	if !ok {
		return "", infraerrors.InternalServer("PRIVACY_RESULT_INVALID", "Privacy result could not be read")
	}
	return mode, nil
}

func (handler *AccountHandler) ReviewCredentialDraft(ctx *gin.Context) {
	var request CreateAccountRequest
	if err := ctx.ShouldBindJSON(&request); err != nil {
		response.BadRequest(ctx, "Invalid account review")
		return
	}
	request.CredentialDraft = ctx.Param("draft")
	binding, err := credentialDraftBinding(ctx, false)
	if err != nil {
		response.ErrorFrom(ctx, err)
		return
	}
	if err := requireCredentialDraftStore(handler.credentialDrafts); err != nil {
		response.ErrorFrom(ctx, err)
		return
	}
	account, revision, err := handler.credentialDrafts.ResolveAccountCredentialDraft(ctx.Request.Context(), request.CredentialDraft, binding)
	if err != nil {
		response.ErrorFrom(ctx, err)
		return
	}
	if revision != request.CredentialDraftRevision {
		response.ErrorFrom(ctx, service.ErrAccountCredentialDraftConflict)
		return
	}
	if err := applyCredentialDraftOptions(account, request); err != nil {
		response.ErrorFrom(ctx, err)
		return
	}
	if err := validateModelCapabilityDefaultsExtra(request.Extra); err != nil {
		response.BadRequest(ctx, err.Error())
		return
	}
	digest, err := credentialDraftSubmission(request)
	if err != nil {
		response.ErrorFrom(ctx, err)
		return
	}
	draft, err := handler.credentialDrafts.ReviewAccountCredentialDraft(ctx.Request.Context(), request.CredentialDraft, binding, revision, digest)
	if err != nil {
		response.ErrorFrom(ctx, err)
		return
	}
	response.Success(ctx, draft)
}
