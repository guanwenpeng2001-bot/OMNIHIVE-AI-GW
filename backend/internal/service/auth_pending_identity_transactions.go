package service

import (
	"context"
	entsql "entgo.io/ent/dialect/sql"
	"errors"
	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/authidentity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/authidentitychannel"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/identityadoptiondecision"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/pendingauthsession"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/predicate"
	dbuser "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/user"
	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
	"strings"
	"time"
)

func cloneOAuthMetadata(values map[string]any) map[string]any {
	if len(values) == 0 {
		return map[string]any{}
	}
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func mergeOAuthMetadata(base map[string]any, overlay map[string]any) map[string]any {
	merged := cloneOAuthMetadata(base)
	for key, value := range overlay {
		merged[key] = value
	}
	return merged
}

func normalizeAdoptedOAuthDisplayName(value string) string {
	value = strings.TrimSpace(value)
	if len([]rune(value)) > 100 {
		value = string([]rune(value)[:100])
	}
	return value
}

func pendingSessionStringValue(values map[string]any, key string) string {
	if len(values) == 0 {
		return ""
	}
	raw, ok := values[key]
	if !ok {
		return ""
	}
	value, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func wechatCompatibleProviderKeys(providerKey string) []string {
	preferred := strings.TrimSpace(providerKey)
	if preferred == "" {
		preferred = "wechat-main"
	}
	keys := []string{preferred}
	if !strings.EqualFold(preferred, "wechat") {
		keys = append(keys, "wechat")
	}
	return keys
}

func UpdatePendingOAuthSessionProgress(
	ctx context.Context,
	client *dbent.Client,
	session *dbent.PendingAuthSession,
	intent string,
	resolvedEmail string,
	targetUserID *int64,
	completionResponse map[string]any,
) (*dbent.PendingAuthSession, error) {
	if client == nil || session == nil {
		return nil, infraerrors.BadRequest("PENDING_AUTH_SESSION_INVALID", "pending auth session is invalid")
	}

	localFlowState := copyPendingMap(session.LocalFlowState)
	localFlowState["completion_response"] = copyPendingMap(completionResponse)

	update := client.PendingAuthSession.UpdateOneID(session.ID).
		SetIntent(strings.TrimSpace(intent)).
		SetResolvedEmail(strings.TrimSpace(resolvedEmail)).
		SetLocalFlowState(localFlowState)
	if targetUserID != nil && *targetUserID > 0 {
		update = update.SetTargetUserID(*targetUserID)
	} else {
		update = update.ClearTargetUserID()
	}
	return update.Save(ctx)
}

func ResolvePendingOAuthTargetUserID(ctx context.Context, client *dbent.Client, session *dbent.PendingAuthSession) (int64, error) {
	if session == nil {
		return 0, infraerrors.BadRequest("PENDING_AUTH_SESSION_INVALID", "pending auth session is invalid")
	}
	if session.TargetUserID != nil && *session.TargetUserID > 0 {
		return *session.TargetUserID, nil
	}
	email := strings.TrimSpace(session.ResolvedEmail)
	if email == "" {
		return 0, infraerrors.BadRequest("PENDING_AUTH_TARGET_USER_MISSING", "pending auth target user is missing")
	}

	userEntity, err := FindUserByNormalizedEmail(ctx, client, email)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return 0, infraerrors.InternalServer("PENDING_AUTH_TARGET_USER_NOT_FOUND", "pending auth target user was not found")
		}
		return 0, err
	}
	return userEntity.ID, nil
}

func UserNormalizedEmailPredicate(email string) predicate.User {
	normalized := strings.ToLower(strings.TrimSpace(email))
	if normalized == "" {
		return dbuser.EmailEQ(email)
	}
	return predicate.User(func(s *entsql.Selector) {
		s.Where(entsql.P(func(b *entsql.Builder) {
			b.WriteString("LOWER(TRIM(").
				Ident(s.C(dbuser.FieldEmail)).
				WriteString(")) = ").
				Arg(normalized)
		}))
	})
}

func FindUserByNormalizedEmail(ctx context.Context, client *dbent.Client, email string) (*dbent.User, error) {
	if client == nil {
		return nil, infraerrors.ServiceUnavailable("PENDING_AUTH_NOT_READY", "pending auth service is not ready")
	}

	matches, err := client.User.Query().
		Where(UserNormalizedEmailPredicate(email)).
		Order(dbent.Asc(dbuser.FieldID)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, ErrUserNotFound
	}
	if len(matches) > 1 {
		return nil, infraerrors.Conflict("USER_EMAIL_CONFLICT", "normalized email matched multiple users")
	}
	return matches[0], nil
}

func EnsurePendingOAuthRegistrationIdentityAvailable(ctx context.Context, client *dbent.Client, session *dbent.PendingAuthSession) error {
	if client == nil || session == nil {
		return infraerrors.BadRequest("PENDING_AUTH_SESSION_INVALID", "pending auth registration context is invalid")
	}

	identity, err := client.AuthIdentity.Query().
		Where(
			authidentity.ProviderTypeEQ(strings.TrimSpace(session.ProviderType)),
			authidentity.ProviderKeyEQ(strings.TrimSpace(session.ProviderKey)),
			authidentity.ProviderSubjectEQ(strings.TrimSpace(session.ProviderSubject)),
		).
		Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil
		}
		return err
	}
	if identity == nil || identity.UserID <= 0 {
		return nil
	}

	activeOwner, err := FindActiveUserByID(ctx, client, identity.UserID)
	if err != nil {
		return err
	}
	if activeOwner != nil {
		return infraerrors.Conflict("AUTH_IDENTITY_OWNERSHIP_CONFLICT", "auth identity already belongs to another user")
	}
	return nil
}

func OauthIdentityIssuer(session *dbent.PendingAuthSession) *string {
	if session == nil {
		return nil
	}
	switch strings.TrimSpace(session.ProviderType) {
	case "oidc":
		issuer := strings.TrimSpace(session.ProviderKey)
		if issuer == "" {
			issuer = pendingSessionStringValue(session.UpstreamIdentityClaims, "issuer")
		}
		if issuer == "" {
			return nil
		}
		return &issuer
	default:
		issuer := pendingSessionStringValue(session.UpstreamIdentityClaims, "issuer")
		if issuer == "" {
			return nil
		}
		return &issuer
	}
}

func EnsurePendingOAuthIdentityForUser(ctx context.Context, tx *dbent.Tx, session *dbent.PendingAuthSession, userID int64) (*dbent.AuthIdentity, error) {
	if session != nil && strings.EqualFold(strings.TrimSpace(session.ProviderType), "wechat") {
		return EnsurePendingWeChatOAuthIdentityForUser(ctx, tx, session, userID)
	}

	client := tx.Client()
	identity, err := client.AuthIdentity.Query().
		Where(
			authidentity.ProviderTypeEQ(strings.TrimSpace(session.ProviderType)),
			authidentity.ProviderKeyEQ(strings.TrimSpace(session.ProviderKey)),
			authidentity.ProviderSubjectEQ(strings.TrimSpace(session.ProviderSubject)),
		).
		Only(ctx)
	if err != nil && !dbent.IsNotFound(err) {
		return nil, err
	}
	if identity != nil {
		if identity.UserID != userID {
			activeOwner, err := FindActiveUserByID(ctx, client, identity.UserID)
			if err != nil {
				return nil, err
			}
			if activeOwner != nil {
				return nil, infraerrors.Conflict("AUTH_IDENTITY_OWNERSHIP_CONFLICT", "auth identity already belongs to another user")
			}
			return client.AuthIdentity.UpdateOneID(identity.ID).
				SetUserID(userID).
				Save(ctx)
		}
		return identity, nil
	}

	create := client.AuthIdentity.Create().
		SetUserID(userID).
		SetProviderType(strings.TrimSpace(session.ProviderType)).
		SetProviderKey(strings.TrimSpace(session.ProviderKey)).
		SetProviderSubject(strings.TrimSpace(session.ProviderSubject)).
		SetMetadata(cloneOAuthMetadata(session.UpstreamIdentityClaims))
	if issuer := OauthIdentityIssuer(session); issuer != nil {
		create = create.SetIssuer(strings.TrimSpace(*issuer))
	}
	return create.Save(ctx)
}

func EnsurePendingWeChatOAuthIdentityForUser(ctx context.Context, tx *dbent.Tx, session *dbent.PendingAuthSession, userID int64) (*dbent.AuthIdentity, error) {
	client := tx.Client()
	providerType := strings.TrimSpace(session.ProviderType)
	providerKey := strings.TrimSpace(session.ProviderKey)
	providerSubject := strings.TrimSpace(session.ProviderSubject)
	providerKeys := wechatCompatibleProviderKeys(providerKey)
	channel := strings.TrimSpace(pendingSessionStringValue(session.UpstreamIdentityClaims, "channel"))
	channelAppID := strings.TrimSpace(pendingSessionStringValue(session.UpstreamIdentityClaims, "channel_app_id"))
	channelSubject := strings.TrimSpace(pendingSessionStringValue(session.UpstreamIdentityClaims, "channel_subject"))
	metadata := cloneOAuthMetadata(session.UpstreamIdentityClaims)

	identityRecords, err := client.AuthIdentity.Query().
		Where(
			authidentity.ProviderTypeEQ(providerType),
			authidentity.ProviderKeyIn(providerKeys...),
			authidentity.ProviderSubjectEQ(providerSubject),
		).
		All(ctx)
	if err != nil {
		return nil, err
	}
	identity, hasCanonicalKey, err := ChooseWeChatIdentityForUser(ctx, client, identityRecords, userID, providerKey)
	if err != nil {
		return nil, err
	}

	var legacyOpenIDIdentity *dbent.AuthIdentity
	if channelSubject != "" && channelSubject != providerSubject {
		legacyOpenIDRecords, err := client.AuthIdentity.Query().
			Where(
				authidentity.ProviderTypeEQ(providerType),
				authidentity.ProviderKeyIn(providerKeys...),
				authidentity.ProviderSubjectEQ(channelSubject),
			).
			All(ctx)
		if err != nil {
			return nil, err
		}
		legacyOpenIDIdentity, _, err = ChooseWeChatIdentityForUser(ctx, client, legacyOpenIDRecords, userID, providerKey)
		if err != nil {
			return nil, err
		}
	}

	switch {
	case identity != nil:
		update := client.AuthIdentity.UpdateOneID(identity.ID).
			SetMetadata(mergeOAuthMetadata(identity.Metadata, metadata))
		if identity.UserID != userID {
			update = update.SetUserID(userID)
		}
		if !strings.EqualFold(strings.TrimSpace(identity.ProviderKey), providerKey) && !hasCanonicalKey {
			update = update.SetProviderKey(providerKey)
		}
		if issuer := OauthIdentityIssuer(session); issuer != nil {
			update = update.SetIssuer(strings.TrimSpace(*issuer))
		}
		identity, err = update.Save(ctx)
		if err != nil {
			return nil, err
		}
	case legacyOpenIDIdentity != nil:
		update := client.AuthIdentity.UpdateOneID(legacyOpenIDIdentity.ID).
			SetProviderKey(providerKey).
			SetProviderSubject(providerSubject).
			SetMetadata(mergeOAuthMetadata(legacyOpenIDIdentity.Metadata, metadata))
		if issuer := OauthIdentityIssuer(session); issuer != nil {
			update = update.SetIssuer(strings.TrimSpace(*issuer))
		}
		identity, err = update.Save(ctx)
		if err != nil {
			return nil, err
		}
	default:
		create := client.AuthIdentity.Create().
			SetUserID(userID).
			SetProviderType(providerType).
			SetProviderKey(providerKey).
			SetProviderSubject(providerSubject).
			SetMetadata(metadata)
		if issuer := OauthIdentityIssuer(session); issuer != nil {
			create = create.SetIssuer(strings.TrimSpace(*issuer))
		}
		identity, err = create.Save(ctx)
		if err != nil {
			return nil, err
		}
	}

	if channel == "" || channelAppID == "" || channelSubject == "" {
		return identity, nil
	}

	channelRecords, err := client.AuthIdentityChannel.Query().
		Where(
			authidentitychannel.ProviderTypeEQ(providerType),
			authidentitychannel.ProviderKeyIn(providerKeys...),
			authidentitychannel.ChannelEQ(channel),
			authidentitychannel.ChannelAppIDEQ(channelAppID),
			authidentitychannel.ChannelSubjectEQ(channelSubject),
		).
		WithIdentity().
		All(ctx)
	if err != nil {
		return nil, err
	}
	channelRecord, hasCanonicalChannelKey, err := ChooseWeChatChannelForUser(ctx, client, channelRecords, userID, providerKey)
	if err != nil {
		return nil, err
	}

	channelMetadata := mergeOAuthMetadata(ChannelRecordMetadata(channelRecord), metadata)
	if channelRecord == nil {
		if _, err := client.AuthIdentityChannel.Create().
			SetIdentityID(identity.ID).
			SetProviderType(providerType).
			SetProviderKey(providerKey).
			SetChannel(channel).
			SetChannelAppID(channelAppID).
			SetChannelSubject(channelSubject).
			SetMetadata(channelMetadata).
			Save(ctx); err != nil {
			return nil, err
		}
		return identity, nil
	}

	updateChannel := client.AuthIdentityChannel.UpdateOneID(channelRecord.ID).
		SetIdentityID(identity.ID).
		SetMetadata(channelMetadata)
	if !strings.EqualFold(strings.TrimSpace(channelRecord.ProviderKey), providerKey) && !hasCanonicalChannelKey {
		updateChannel = updateChannel.SetProviderKey(providerKey)
	}
	_, err = updateChannel.Save(ctx)
	if err != nil {
		return nil, err
	}
	return identity, nil
}

func ChooseWeChatIdentityForUser(ctx context.Context, client *dbent.Client, records []*dbent.AuthIdentity, userID int64, preferredProviderKey string) (*dbent.AuthIdentity, bool, error) {
	var preferred *dbent.AuthIdentity
	var fallback *dbent.AuthIdentity
	hasCanonicalKey := false
	for _, record := range records {
		if record == nil {
			continue
		}
		if record.UserID != userID {
			activeOwner, err := FindActiveUserByID(ctx, client, record.UserID)
			if err != nil {
				return nil, false, err
			}
			if activeOwner != nil {
				return nil, false, infraerrors.Conflict("AUTH_IDENTITY_OWNERSHIP_CONFLICT", "auth identity already belongs to another user")
			}
		}
		if strings.EqualFold(strings.TrimSpace(record.ProviderKey), preferredProviderKey) {
			hasCanonicalKey = true
			if preferred == nil {
				preferred = record
			}
			continue
		}
		if fallback == nil {
			fallback = record
		}
	}
	if preferred != nil {
		return preferred, hasCanonicalKey, nil
	}
	return fallback, hasCanonicalKey, nil
}

func ChooseWeChatChannelForUser(ctx context.Context, client *dbent.Client, records []*dbent.AuthIdentityChannel, userID int64, preferredProviderKey string) (*dbent.AuthIdentityChannel, bool, error) {
	var preferred *dbent.AuthIdentityChannel
	var fallback *dbent.AuthIdentityChannel
	hasCanonicalKey := false
	for _, record := range records {
		if record == nil {
			continue
		}
		if record.Edges.Identity != nil && record.Edges.Identity.UserID != userID {
			activeOwner, err := FindActiveUserByID(ctx, client, record.Edges.Identity.UserID)
			if err != nil {
				return nil, false, err
			}
			if activeOwner != nil {
				return nil, false, infraerrors.Conflict("AUTH_IDENTITY_CHANNEL_OWNERSHIP_CONFLICT", "auth identity channel already belongs to another user")
			}
		}
		if strings.EqualFold(strings.TrimSpace(record.ProviderKey), preferredProviderKey) {
			hasCanonicalKey = true
			if preferred == nil {
				preferred = record
			}
			continue
		}
		if fallback == nil {
			fallback = record
		}
	}
	if preferred != nil {
		return preferred, hasCanonicalKey, nil
	}
	return fallback, hasCanonicalKey, nil
}

func FindActiveUserByID(ctx context.Context, client *dbent.Client, userID int64) (*dbent.User, error) {
	if client == nil || userID <= 0 {
		return nil, nil
	}
	userEntity, err := client.User.Get(ctx, userID)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, nil
		}
		return nil, infraerrors.InternalServer("AUTH_IDENTITY_USER_LOOKUP_FAILED", "failed to load auth identity user").WithCause(err)
	}
	if !strings.EqualFold(strings.TrimSpace(userEntity.Status), StatusActive) {
		return nil, ErrUserNotActive
	}
	return userEntity, nil
}

func ChannelRecordMetadata(channel *dbent.AuthIdentityChannel) map[string]any {
	if channel == nil {
		return map[string]any{}
	}
	return cloneOAuthMetadata(channel.Metadata)
}

func ShouldBindPendingOAuthIdentity(session *dbent.PendingAuthSession, decision *dbent.IdentityAdoptionDecision) bool {
	if session == nil || decision == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(session.Intent)) {
	case "bind_current_user", "login", "adopt_existing_user_by_email":
		return true
	default:
		return decision.AdoptDisplayName || decision.AdoptAvatar
	}
}

func ShouldSkipAvatarAdoption(err error) bool {
	return errors.Is(err, ErrAvatarInvalid) ||
		errors.Is(err, ErrAvatarTooLarge) ||
		errors.Is(err, ErrAvatarNotImage)
}

func ApplyPendingOAuthBinding(
	ctx context.Context,
	client *dbent.Client,
	authService *AuthService,
	userService *UserService,
	session *dbent.PendingAuthSession,
	decision *dbent.IdentityAdoptionDecision,
	overrideUserID *int64,
	forceBind bool,
	applyFirstBindDefaults bool,
) error {
	if client == nil || session == nil {
		return nil
	}
	if !forceBind && !ShouldBindPendingOAuthIdentity(session, decision) {
		return nil
	}

	if tx := dbent.TxFromContext(ctx); tx != nil {
		return ApplyPendingOAuthBindingTx(ctx, tx, authService, userService, session, decision, overrideUserID, forceBind, applyFirstBindDefaults)
	}

	tx, err := client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	txCtx := dbent.NewTxContext(ctx, tx)
	if err := ApplyPendingOAuthBindingTx(txCtx, tx, authService, userService, session, decision, overrideUserID, forceBind, applyFirstBindDefaults); err != nil {
		return err
	}
	return tx.Commit()
}

func ApplyPendingOAuthBindingTx(
	ctx context.Context,
	tx *dbent.Tx,
	authService *AuthService,
	userService *UserService,
	session *dbent.PendingAuthSession,
	decision *dbent.IdentityAdoptionDecision,
	overrideUserID *int64,
	forceBind bool,
	applyFirstBindDefaults bool,
) error {
	if tx == nil || session == nil {
		return nil
	}
	if !forceBind && !ShouldBindPendingOAuthIdentity(session, decision) {
		return nil
	}

	targetUserID := int64(0)
	if overrideUserID != nil && *overrideUserID > 0 {
		targetUserID = *overrideUserID
	} else {
		resolvedUserID, err := ResolvePendingOAuthTargetUserID(ctx, tx.Client(), session)
		if err != nil {
			return err
		}
		targetUserID = resolvedUserID
	}

	adoptedDisplayName := ""
	if decision != nil && decision.AdoptDisplayName {
		adoptedDisplayName = normalizeAdoptedOAuthDisplayName(pendingSessionStringValue(session.UpstreamIdentityClaims, "suggested_display_name"))
	}
	adoptedAvatarURL := ""
	if decision != nil && decision.AdoptAvatar {
		adoptedAvatarURL = pendingSessionStringValue(session.UpstreamIdentityClaims, "suggested_avatar_url")
	}
	shouldAdoptAvatar := false
	if decision != nil && decision.AdoptAvatar && adoptedAvatarURL != "" {
		if err := ValidateUserAvatar(adoptedAvatarURL); err == nil {
			shouldAdoptAvatar = true
		} else if !ShouldSkipAvatarAdoption(err) {
			return err
		}
	}

	if decision != nil && decision.AdoptDisplayName && adoptedDisplayName != "" {
		if err := tx.Client().User.UpdateOneID(targetUserID).
			SetUsername(adoptedDisplayName).
			Exec(ctx); err != nil {
			return err
		}
	}

	identity, err := EnsurePendingOAuthIdentityForUser(ctx, tx, session, targetUserID)
	if err != nil {
		return err
	}

	metadata := cloneOAuthMetadata(identity.Metadata)
	for key, value := range session.UpstreamIdentityClaims {
		metadata[key] = value
	}
	if decision != nil && decision.AdoptDisplayName && adoptedDisplayName != "" {
		metadata["display_name"] = adoptedDisplayName
	}
	if shouldAdoptAvatar {
		metadata["avatar_url"] = adoptedAvatarURL
	}

	updateIdentity := tx.Client().AuthIdentity.UpdateOneID(identity.ID).SetMetadata(metadata)
	if issuer := OauthIdentityIssuer(session); issuer != nil {
		updateIdentity = updateIdentity.SetIssuer(strings.TrimSpace(*issuer))
	}
	if _, err := updateIdentity.Save(ctx); err != nil {
		return err
	}

	if decision != nil && (decision.IdentityID == nil || *decision.IdentityID != identity.ID) {
		if _, err := tx.Client().IdentityAdoptionDecision.Update().
			Where(
				identityadoptiondecision.IdentityIDEQ(identity.ID),
				identityadoptiondecision.IDNEQ(decision.ID),
			).
			ClearIdentityID().
			Save(ctx); err != nil {
			return err
		}
		if _, err := tx.Client().IdentityAdoptionDecision.UpdateOneID(decision.ID).
			SetIdentityID(identity.ID).
			Save(ctx); err != nil {
			return err
		}
	}

	if applyFirstBindDefaults && authService != nil {
		if err := authService.ApplyProviderDefaultSettingsOnFirstBind(ctx, targetUserID, session.ProviderType); err != nil {
			return err
		}
	}

	if shouldAdoptAvatar && userService != nil {
		if _, err := userService.SetAvatar(ctx, targetUserID, adoptedAvatarURL); err != nil {
			return err
		}
	}

	return nil
}

func ConsumePendingOAuthBrowserSessionTx(
	ctx context.Context,
	tx *dbent.Tx,
	session *dbent.PendingAuthSession,
) error {
	if tx == nil || session == nil {
		return ErrPendingAuthSessionNotFound
	}

	storedSession, err := tx.Client().PendingAuthSession.Get(ctx, session.ID)
	if err != nil {
		if dbent.IsNotFound(err) {
			return ErrPendingAuthSessionNotFound
		}
		return err
	}

	if err := validatePendingSessionState(storedSession, session.BrowserSessionKey, ErrPendingAuthSessionExpired, ErrPendingAuthSessionConsumed); err != nil {
		return err
	}
	now := time.Now().UTC()

	affected, err := tx.Client().PendingAuthSession.Update().Where(
		pendingauthsession.IDEQ(storedSession.ID),
		pendingauthsession.SessionTokenEQ(session.SessionToken),
		pendingauthsession.BrowserSessionKeyEQ(session.BrowserSessionKey),
		pendingauthsession.ConsumedAtIsNil(),
		pendingauthsession.ExpiresAtGT(now),
		pendingauthsession.Or(pendingauthsession.CompletionCodeExpiresAtIsNil(), pendingauthsession.CompletionCodeExpiresAtGT(now)),
	).SetConsumedAt(now).SetLocalFlowState(sanitizePendingAuthLocalFlowState(storedSession.LocalFlowState)).SetCompletionCodeHash("").ClearCompletionCodeExpiresAt().Save(ctx)
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrPendingAuthSessionConsumed
	}

	return nil
}

func ApplyPendingOAuthAdoptionAndConsumeSession(
	ctx context.Context,
	client *dbent.Client,
	authService *AuthService,
	userService *UserService,
	session *dbent.PendingAuthSession,
	decision *dbent.IdentityAdoptionDecision,
	userID int64,
) error {
	if client == nil {
		return infraerrors.ServiceUnavailable("PENDING_AUTH_NOT_READY", "pending auth service is not ready")
	}
	if session == nil || userID <= 0 {
		return infraerrors.BadRequest("PENDING_AUTH_SESSION_INVALID", "pending auth registration context is invalid")
	}

	tx, err := client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	txCtx := dbent.NewTxContext(ctx, tx)
	if err := ConsumePendingOAuthBrowserSessionTx(txCtx, tx, session); err != nil {
		return err
	}
	if err := ApplyPendingOAuthAdoption(txCtx, client, authService, userService, session, decision, &userID); err != nil {
		return err
	}

	return tx.Commit()
}

func ApplyPendingOAuthAdoption(
	ctx context.Context,
	client *dbent.Client,
	authService *AuthService,
	userService *UserService,
	session *dbent.PendingAuthSession,
	decision *dbent.IdentityAdoptionDecision,
	overrideUserID *int64,
) error {
	return ApplyPendingOAuthBinding(
		ctx,
		client,
		authService,
		userService,
		session,
		decision,
		overrideUserID,
		false,
		strings.EqualFold(strings.TrimSpace(session.Intent), "bind_current_user"),
	)
}

func (s *AuthService) UpdatePendingOAuthSessionProgress(ctx context.Context, session *dbent.PendingAuthSession, intent, email string, userID *int64, completion map[string]any) (*dbent.PendingAuthSession, error) {
	return UpdatePendingOAuthSessionProgress(ctx, s.entClient, session, intent, email, userID, completion)
}
func (s *AuthService) FindUserByNormalizedEmail(ctx context.Context, email string) (*dbent.User, error) {
	return FindUserByNormalizedEmail(ctx, s.entClient, email)
}
func (s *AuthService) FindActiveUserByID(ctx context.Context, userID int64) (*dbent.User, error) {
	return FindActiveUserByID(ctx, s.entClient, userID)
}
func (s *AuthService) EnsurePendingOAuthRegistrationIdentityAvailable(ctx context.Context, session *dbent.PendingAuthSession) error {
	return EnsurePendingOAuthRegistrationIdentityAvailable(ctx, s.entClient, session)
}
func (s *AuthService) ApplyPendingOAuthBinding(ctx context.Context, users *UserService, session *dbent.PendingAuthSession, decision *dbent.IdentityAdoptionDecision, overrideUserID *int64, forceBind, applyDefaults bool) error {
	return ApplyPendingOAuthBinding(ctx, s.entClient, s, users, session, decision, overrideUserID, forceBind, applyDefaults)
}
func (s *AuthService) ApplyPendingOAuthAdoption(ctx context.Context, users *UserService, session *dbent.PendingAuthSession, decision *dbent.IdentityAdoptionDecision, overrideUserID *int64) error {
	return ApplyPendingOAuthAdoption(ctx, s.entClient, s, users, session, decision, overrideUserID)
}
func (s *AuthService) ApplyPendingOAuthAdoptionAndConsumeSession(ctx context.Context, users *UserService, session *dbent.PendingAuthSession, decision *dbent.IdentityAdoptionDecision, userID int64) error {
	return ApplyPendingOAuthAdoptionAndConsumeSession(ctx, s.entClient, s, users, session, decision, userID)
}

func (s *AuthService) FindOAuthIdentityUser(ctx context.Context, identity PendingAuthIdentityKey) (*dbent.User, error) {
	if s == nil || s.entClient == nil {
		return nil, infraerrors.ServiceUnavailable("PENDING_AUTH_NOT_READY", "pending auth service is not ready")
	}
	record, err := s.entClient.AuthIdentity.Query().Where(authidentity.ProviderTypeEQ(strings.TrimSpace(identity.ProviderType)), authidentity.ProviderKeyEQ(strings.TrimSpace(identity.ProviderKey)), authidentity.ProviderSubjectEQ(strings.TrimSpace(identity.ProviderSubject))).Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, nil
		}
		return nil, infraerrors.InternalServer("AUTH_IDENTITY_LOOKUP_FAILED", "failed to inspect auth identity ownership").WithCause(err)
	}
	return FindActiveUserByID(ctx, s.entClient, record.UserID)
}
func (s *AuthService) LoadPendingAdoptionDecision(ctx context.Context, sessionID int64) (*dbent.IdentityAdoptionDecision, error) {
	if s == nil || s.entClient == nil {
		return nil, infraerrors.ServiceUnavailable("PENDING_AUTH_NOT_READY", "pending auth service is not ready")
	}
	return s.entClient.IdentityAdoptionDecision.Query().Where(identityadoptiondecision.PendingAuthSessionIDEQ(sessionID)).Only(ctx)
}

func (s *AuthService) PendingOAuthIdentityExistsForUser(
	ctx context.Context,
	session *dbent.PendingAuthSession,
	userID int64,
) (bool, error) {
	client := s.entClient
	if client == nil || session == nil || userID <= 0 {
		return false, nil
	}

	providerType := strings.TrimSpace(session.ProviderType)
	providerKey := strings.TrimSpace(session.ProviderKey)
	providerSubject := strings.TrimSpace(session.ProviderSubject)
	if providerType == "" || providerSubject == "" {
		return false, nil
	}

	query := client.AuthIdentity.Query().
		Where(
			authidentity.ProviderTypeEQ(providerType),
			authidentity.ProviderSubjectEQ(providerSubject),
			authidentity.UserIDEQ(userID),
		)
	if strings.EqualFold(providerType, "wechat") {
		query = query.Where(authidentity.ProviderKeyIn(wechatCompatibleProviderKeys(providerKey)...))
	} else if providerKey != "" {
		query = query.Where(authidentity.ProviderKeyEQ(providerKey))
	}

	count, err := query.Count(ctx)
	if err != nil {
		return false, infraerrors.InternalServer("AUTH_IDENTITY_LOOKUP_FAILED", "failed to inspect auth identity ownership").WithCause(err)
	}
	return count > 0, nil
}

// FinalizePendingOAuthRegistration preserves the existing bind/finalize/consume
// transaction; the handler owns only HTTP errors and compensating account cleanup.
func (s *AuthService) FinalizePendingOAuthRegistration(ctx context.Context, users *UserService, session *dbent.PendingAuthSession, decision *dbent.IdentityAdoptionDecision, user *User, invitationCode, affiliateCode string, beforeCommit func(context.Context, *dbent.PendingAuthSession) error) (bool, error) {
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return false, pendingOAuthBindingError(err)
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	if err := ConsumePendingOAuthBrowserSessionTx(txCtx, tx, session); err != nil {
		return true, err
	}
	if err := ApplyPendingOAuthBinding(txCtx, s.entClient, s, users, session, decision, &user.ID, true, false); err != nil {
		return false, pendingOAuthBindingError(err)
	}
	if err := s.FinalizeOAuthEmailAccount(txCtx, user, strings.TrimSpace(invitationCode), strings.TrimSpace(session.ProviderType), strings.TrimSpace(affiliateCode)); err != nil {
		return false, err
	}

	if beforeCommit != nil {
		if err := beforeCommit(txCtx, session); err != nil {
			return false, pendingOAuthBindingError(err)
		}
	}
	if err := tx.Commit(); err != nil {
		return false, pendingOAuthBindingError(err)
	}
	return false, nil
}
func pendingOAuthBindingError(err error) error {
	if code := infraerrors.Code(err); code >= 400 && code < 500 {
		return err
	}
	return infraerrors.InternalServer("PENDING_AUTH_BIND_APPLY_FAILED", "failed to bind pending oauth identity").WithCause(err)
}

func (authService *AuthService) CompletePendingOAuthBindLogin(ctx context.Context, users *UserService, session *dbent.PendingAuthSession, decision *dbent.IdentityAdoptionDecision, userID int64) error {
	transaction, err := authService.entClient.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = transaction.Rollback() }()
	transactionContext := dbent.NewTxContext(ctx, transaction)
	if err := ConsumePendingOAuthBrowserSessionTx(transactionContext, transaction, session); err != nil {
		return err
	}
	if err := ApplyPendingOAuthBindingTx(transactionContext, transaction, authService, users, session, decision, &userID, true, true); err != nil {
		return pendingOAuthBindingError(err)
	}
	return transaction.Commit()
}
