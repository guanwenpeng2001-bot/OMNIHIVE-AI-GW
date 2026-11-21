package service

import (
	"context"

	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
)

func groupOnlyAccountUpdate(input *UpdateAccountInput) bool {
	return input != nil && input.GroupIDs != nil &&
		input.TPMLimit == nil && input.Name == "" && input.Notes == nil && input.Type == "" &&
		len(input.Credentials) == 0 && input.Extra == nil && input.ProxyID == nil && input.ProxyGroupID == nil &&
		input.Concurrency == nil && input.Priority == nil && input.RateMultiplier == nil && input.LoadFactor == nil &&
		input.Status == "" && input.ExpiresAt == nil && input.AutoPauseOnExpired == nil &&
		input.ProbeEnabled == nil && input.RateSyncEnabled == nil
}

func (s *adminServiceImpl) updateAccountGroupsOnly(ctx context.Context, account *Account, input *UpdateAccountInput) (*Account, error) {
	if err := s.validateGroupIDsExist(ctx, *input.GroupIDs); err != nil {
		return nil, err
	}
	if err := s.ValidateAccountGroupBindings(ctx, *input.GroupIDs); err != nil {
		return nil, err
	}
	if !input.SkipMixedChannelCheck {
		if err := s.checkMixedChannelRisk(ctx, account.ID, account.Platform, *input.GroupIDs); err != nil {
			return nil, err
		}
	}
	if err := s.accountRepo.BindGroups(ctx, account.ID, *input.GroupIDs); err != nil {
		return nil, err
	}
	account.GroupIDs = *input.GroupIDs
	if dbent.TxFromContext(ctx) == nil {
		InvalidateAvailableModelsCacheForAccount(account)
	}
	return s.accountRepo.GetByID(ctx, account.ID)
}
