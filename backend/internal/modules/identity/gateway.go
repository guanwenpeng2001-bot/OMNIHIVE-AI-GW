package identity

import (
	"context"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
)

type KeyFacts struct {
	Status        string
	IPAllowed     bool
	HasUser       bool
	UserActive    bool
	GroupDeleted  bool
	GroupDisabled bool
	GroupAllowed  bool
	Expired       bool
	Exhausted     bool
}

func EvaluateKey(k KeyFacts) *fault.Fault {
	switch {
	case k.Status != "active" && k.Status != "expired" && k.Status != "quota_exhausted":
		return &fault.Fault{Status: 401, Reason: "API_KEY_DISABLED", Message: "API key is disabled"}
	case !k.IPAllowed:
		return &fault.Fault{Status: 403, Reason: "ACCESS_DENIED", Message: "Access denied"}
	case !k.HasUser:
		return &fault.Fault{Status: 401, Reason: "USER_NOT_FOUND", Message: "User associated with API key not found"}
	case !k.UserActive:
		return &fault.Fault{Status: 401, Reason: "USER_INACTIVE", Message: "User account is not active"}
	case k.GroupDeleted:
		return &fault.Fault{Status: 403, Reason: "GROUP_DELETED", Message: "API key group has been deleted"}
	case k.GroupDisabled:
		return &fault.Fault{Status: 403, Reason: "GROUP_DISABLED", Message: "API key group is disabled"}
	case !k.GroupAllowed:
		return &fault.Fault{Status: 403, Reason: "GROUP_NOT_ALLOWED", Message: "API key group is no longer available to this user"}
	}
	return nil
}

func EvaluateKeyLimits(k KeyFacts) *fault.Fault {
	switch {
	case k.Status == "quota_exhausted":
		return keyQuota()
	case k.Status == "expired" || k.Expired:
		return &fault.Fault{Status: 403, Reason: "API_KEY_EXPIRED", Message: "API key has expired"}
	case k.Exhausted:
		return keyQuota()
	}
	return nil
}

func keyQuota() *fault.Fault {
	return &fault.Fault{Status: 429, Reason: "API_KEY_QUOTA_EXHAUSTED", Message: "API key quota exhausted"}
}

type FundingChecks struct {
	Diagnostic  bool
	BillingInfo bool
	Simple      bool
	LimitsFirst bool
}

type SubscriptionReader[T any] interface {
	Active(context.Context) (*T, error)
	Validate(context.Context, *T) (*T, *fault.Fault)
}

func EvaluateFunding[T any](ctx context.Context, facts KeyFacts, hasBalance bool, checks FundingChecks, subscriptions SubscriptionReader[T]) (*T, *fault.Fault) {
	limits := EvaluateKeyLimits(facts)
	if checks.LimitsFirst && !checks.Diagnostic && limits != nil {
		return nil, limits
	}
	if checks.Simple {
		return nil, nil
	}
	var sub *T
	if subscriptions != nil && !checks.BillingInfo {
		var err error
		sub, err = subscriptions.Active(ctx)
		if err != nil {
			if checks.Diagnostic {
				return nil, nil
			}
			return nil, &fault.Fault{Status: 403, Reason: "SUBSCRIPTION_NOT_FOUND", Message: "No active subscription found for this group"}
		}
	}
	if checks.Diagnostic {
		return sub, nil
	}
	if limits != nil {
		return nil, limits
	}
	if sub != nil {
		return subscriptions.Validate(ctx, sub)
	}
	if !hasBalance {
		return nil, &fault.Fault{Status: 403, Reason: "INSUFFICIENT_BALANCE", Message: "Insufficient account balance"}
	}
	return nil, nil
}
