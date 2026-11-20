package identitycompat

import (
	"context"
	"errors"
	"strings"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

func ProjectKey(ctx context.Context, keys *service.APIKeyService, p *identity.KeyPolicy) (*service.APIKey, error) {
	k, err := keys.GetByID(ctx, p.Scope.KeyID)
	if err != nil {
		return nil, err
	}
	if k == nil || k.WorkspaceID != p.Scope.WorkspaceID || k.UserID != p.Scope.KeyOwnerID {
		return nil, service.ErrAPIKeyNotFound
	}
	if k.User != nil {
		k.User.AllowedGroups = p.AllowedGroups
		k.User.PasswordHash = ""
		k.User.TotpSecretEncrypted = nil
		if k.GroupID != nil && *k.GroupID == p.GroupID {
			k.User.UserGroupRPMOverride = p.GroupRPMOverride
		}
	}
	return k, nil
}

func KeyFacts(key *service.APIKey, ipAllowed bool) identity.KeyFacts {
	facts := identity.KeyFacts{Status: key.Status, IPAllowed: ipAllowed, HasUser: key.User != nil,
		UserActive: key.User != nil && key.User.IsActive(), GroupAllowed: true, Expired: key.IsExpired(), Exhausted: key.IsQuotaExhausted()}
	if key.GroupID != nil {
		facts.GroupDeleted = key.Group == nil || strings.EqualFold(key.Group.Status, "deleted")
		if key.Group != nil {
			facts.GroupDisabled = !key.Group.IsActive()
			facts.GroupAllowed = key.User == nil || key.Group.IsSubscriptionType() || key.User.CanBindGroup(key.Group.ID, key.Group.IsExclusive)
		}
	}
	return facts
}

type subscriptionReader struct {
	svc *service.SubscriptionService
	key *service.APIKey
}

func Subscriptions(key *service.APIKey, svc *service.SubscriptionService) identity.SubscriptionReader[service.UserSubscription] {
	if svc == nil || key.Group == nil || !key.Group.IsSubscriptionType() {
		return nil
	}
	return subscriptionReader{svc, key}
}

func (r subscriptionReader) Active(ctx context.Context) (*service.UserSubscription, error) {
	return r.svc.GetActiveSubscription(ctx, r.key.User.ID, r.key.Group.ID)
}

func (r subscriptionReader) Validate(ctx context.Context, sub *service.UserSubscription) (*service.UserSubscription, *fault.Fault) {
	maintain, err := r.svc.ValidateAndCheckLimits(sub, r.key.Group)
	if maintain {
		sub, err = r.svc.EnsureWindowMaintenance(ctx, sub)
		if err != nil {
			return nil, &fault.Fault{Status: 500, Reason: "SUBSCRIPTION_MAINTENANCE_FAILED", Message: "Failed to maintain subscription usage windows"}
		}
		_, err = r.svc.ValidateAndCheckLimits(sub, r.key.Group)
	}
	if errors.Is(err, service.ErrDailyLimitExceeded) || errors.Is(err, service.ErrWeeklyLimitExceeded) || errors.Is(err, service.ErrMonthlyLimitExceeded) {
		return nil, &fault.Fault{Status: 429, Reason: "USAGE_LIMIT_EXCEEDED", Message: err.Error()}
	}
	if err != nil {
		return nil, &fault.Fault{Status: 403, Reason: "SUBSCRIPTION_INVALID", Message: err.Error()}
	}
	return sub, nil
}
