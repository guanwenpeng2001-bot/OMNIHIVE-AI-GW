package service

import (
	"context"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/authidentity"
	"github.com/Wei-Shaw/sub2api/ent/authidentitychannel"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"strings"
)

const wechatOAuthProviderKey = "wechat-main"

func (s *AuthService) EnsureWeChatBindOwnership(
	ctx context.Context,
	userID int64,
	providerSubject string,
	channelMode string, channelAppID string,
	channelSubject string,
) error {
	client := s.entClient
	if client == nil {
		return infraerrors.ServiceUnavailable("PENDING_AUTH_NOT_READY", "pending auth service is not ready")
	}

	identities, err := client.AuthIdentity.Query().
		Where(
			authidentity.ProviderTypeEQ("wechat"),
			authidentity.ProviderKeyIn(wechatCompatibleProviderKeys(wechatOAuthProviderKey)...),
			authidentity.ProviderSubjectEQ(strings.TrimSpace(providerSubject)),
		).
		All(ctx)
	if err != nil {
		return infraerrors.InternalServer("WECHAT_BIND_LOOKUP_FAILED", "failed to inspect wechat identity ownership").WithCause(err)
	}
	for _, identity := range identities {
		if identity != nil && identity.UserID != userID {
			activeOwner, lookupErr := s.FindActiveUserByID(ctx, identity.UserID)
			if lookupErr != nil {
				return lookupErr
			}
			if activeOwner != nil {
				return infraerrors.Conflict("AUTH_IDENTITY_OWNERSHIP_CONFLICT", "auth identity already belongs to another user")
			}
		}
	}

	channelSubject = strings.TrimSpace(channelSubject)
	channelAppID = strings.TrimSpace(channelAppID)
	if channelSubject == "" || channelAppID == "" {
		return nil
	}

	channels, err := client.AuthIdentityChannel.Query().
		Where(
			authidentitychannel.ProviderTypeEQ("wechat"),
			authidentitychannel.ProviderKeyIn(wechatCompatibleProviderKeys(wechatOAuthProviderKey)...),
			authidentitychannel.ChannelEQ(strings.TrimSpace(channelMode)),
			authidentitychannel.ChannelAppIDEQ(channelAppID),
			authidentitychannel.ChannelSubjectEQ(channelSubject),
		).
		WithIdentity().
		All(ctx)
	if err != nil {
		return infraerrors.InternalServer("WECHAT_BIND_CHANNEL_LOOKUP_FAILED", "failed to inspect wechat identity channel ownership").WithCause(err)
	}
	for _, channel := range channels {
		if channel != nil && channel.Edges.Identity != nil && channel.Edges.Identity.UserID != userID {
			activeOwner, lookupErr := s.FindActiveUserByID(ctx, channel.Edges.Identity.UserID)
			if lookupErr != nil {
				return lookupErr
			}
			if activeOwner != nil {
				return infraerrors.Conflict("AUTH_IDENTITY_CHANNEL_OWNERSHIP_CONFLICT", "auth identity channel already belongs to another user")
			}
		}
	}
	return nil
}

func (s *AuthService) FindWeChatUserByLegacyOpenID(
	ctx context.Context,
	identity PendingAuthIdentityKey,
	channelMode string, channelAppID string,
	openid string,
) (*dbent.User, error) {
	client := s.entClient
	if client == nil {
		return nil, infraerrors.ServiceUnavailable("PENDING_AUTH_NOT_READY", "pending auth service is not ready")
	}

	providerType := strings.TrimSpace(identity.ProviderType)
	providerSubject := strings.TrimSpace(identity.ProviderSubject)
	providerKeys := wechatCompatibleProviderKeys(identity.ProviderKey)
	if providerSubject != "" {
		records, err := client.AuthIdentity.Query().
			Where(
				authidentity.ProviderTypeEQ(providerType),
				authidentity.ProviderKeyIn(providerKeys...),
				authidentity.ProviderSubjectEQ(providerSubject),
			).
			WithUser().
			All(ctx)
		if err != nil {
			return nil, infraerrors.InternalServer("AUTH_IDENTITY_LOOKUP_FAILED", "failed to inspect auth identity ownership").WithCause(err)
		}
		if user, err := singleWeChatIdentityUser(records); err != nil || user != nil {
			if err != nil || user == nil {
				return user, err
			}
			return s.FindActiveUserByID(ctx, user.ID)
		}
	}

	openid = strings.TrimSpace(openid)
	channel := strings.TrimSpace(channelMode)
	channelAppID = strings.TrimSpace(channelAppID)
	if openid != "" && channel != "" && channelAppID != "" {
		records, err := client.AuthIdentityChannel.Query().
			Where(
				authidentitychannel.ProviderTypeEQ(providerType),
				authidentitychannel.ProviderKeyIn(providerKeys...),
				authidentitychannel.ChannelEQ(channel),
				authidentitychannel.ChannelAppIDEQ(channelAppID),
				authidentitychannel.ChannelSubjectEQ(openid),
			).
			WithIdentity(func(q *dbent.AuthIdentityQuery) {
				q.WithUser()
			}).
			All(ctx)
		if err != nil {
			return nil, infraerrors.InternalServer("AUTH_IDENTITY_CHANNEL_LOOKUP_FAILED", "failed to inspect auth identity channel ownership").WithCause(err)
		}
		if user, err := singleWeChatChannelUser(records); err != nil || user != nil {
			if err != nil || user == nil {
				return user, err
			}
			return s.FindActiveUserByID(ctx, user.ID)
		}
	}

	if openid == "" {
		return nil, nil
	}

	records, err := client.AuthIdentity.Query().
		Where(
			authidentity.ProviderTypeEQ(providerType),
			authidentity.ProviderKeyIn(providerKeys...),
			authidentity.ProviderSubjectEQ(openid),
		).
		WithUser().
		All(ctx)
	if err != nil {
		return nil, infraerrors.InternalServer("AUTH_IDENTITY_LOOKUP_FAILED", "failed to inspect auth identity ownership").WithCause(err)
	}
	user, err := singleWeChatIdentityUser(records)
	if err != nil || user == nil {
		return user, err
	}
	return s.FindActiveUserByID(ctx, user.ID)
}

func (s *AuthService) EnsureWeChatRuntimeIdentityBinding(
	ctx context.Context,
	userID int64,
	identity PendingAuthIdentityKey,
	upstreamClaims map[string]any,
) error {
	client := s.entClient
	if client == nil {
		return infraerrors.ServiceUnavailable("PENDING_AUTH_NOT_READY", "pending auth service is not ready")
	}

	tx, err := client.Tx(ctx)
	if err != nil {
		return infraerrors.InternalServer("AUTH_IDENTITY_BIND_FAILED", "failed to begin wechat identity repair transaction").WithCause(err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = EnsurePendingOAuthIdentityForUser(dbent.NewTxContext(ctx, tx), tx, &dbent.PendingAuthSession{
		ProviderType:           strings.TrimSpace(identity.ProviderType),
		ProviderKey:            strings.TrimSpace(identity.ProviderKey),
		ProviderSubject:        strings.TrimSpace(identity.ProviderSubject),
		UpstreamIdentityClaims: cloneOAuthMetadata(upstreamClaims),
	}, userID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func singleWeChatIdentityUser(records []*dbent.AuthIdentity) (*dbent.User, error) {
	var resolved *dbent.User
	for _, record := range records {
		if record == nil || record.Edges.User == nil {
			continue
		}
		if resolved == nil {
			resolved = record.Edges.User
			continue
		}
		if resolved.ID != record.Edges.User.ID {
			return nil, infraerrors.Conflict("AUTH_IDENTITY_OWNERSHIP_CONFLICT", "auth identity already belongs to another user")
		}
	}
	return resolved, nil
}

func singleWeChatChannelUser(records []*dbent.AuthIdentityChannel) (*dbent.User, error) {
	var resolved *dbent.User
	for _, record := range records {
		if record == nil || record.Edges.Identity == nil || record.Edges.Identity.Edges.User == nil {
			continue
		}
		if resolved == nil {
			resolved = record.Edges.Identity.Edges.User
			continue
		}
		if resolved.ID != record.Edges.Identity.Edges.User.ID {
			return nil, infraerrors.Conflict("AUTH_IDENTITY_CHANNEL_OWNERSHIP_CONFLICT", "auth identity channel already belongs to another user")
		}
	}
	return resolved, nil
}

func (s *AuthService) ReadOAuthBindTargetUser(ctx context.Context, userID int64) (*dbent.User, error) {
	if s == nil || s.entClient == nil {
		return nil, infraerrors.ServiceUnavailable("PENDING_AUTH_NOT_READY", "pending auth service is not ready")
	}
	user, err := s.entClient.User.Get(ctx, userID)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, infraerrors.Unauthorized("AUTH_REQUIRED", "current user is required to bind wechat account")
		}
		return nil, infraerrors.InternalServer("WECHAT_BIND_USER_LOOKUP_FAILED", "failed to load current user").WithCause(err)
	}
	return user, nil
}
