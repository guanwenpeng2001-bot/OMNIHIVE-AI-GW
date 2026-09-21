package service

import (
	"context"
	"errors"
)

// 管理端预算写入口。
//
// key-budget-periods 已经把服务端能力做好（SaveBudgetPolicy / AdjustBudgetUsage），
// 但只有用户自助路径，管理端没有写入口。这里补上接线，并刻意复用同一套校验与
// 持久化实现（SaveAPIKeyBudgetPolicy / AdjustAPIKeyBudgetUsage），
// 不另造一套管理通道，也不放宽任何约束。

// budgetRepository 复用与 APIKeyService 相同的窄扩展口判定：注入的 API key 仓储
// 实现 APIKeyBudgetRepository 时才有预算能力。
func (s *adminServiceImpl) budgetRepository() (APIKeyBudgetRepository, error) {
	if s == nil || s.apiKeyRepo == nil {
		return nil, errors.New("api key budget repository unavailable")
	}
	repo, ok := s.apiKeyRepo.(APIKeyBudgetRepository)
	if !ok {
		return nil, errors.New("api key budget repository unavailable")
	}
	return repo, nil
}

// AdminSetAPIKeyBudgetPolicy 设置任意 Key 的周期预算策略，包含预算开关。
// enforcedChangeAllowed=true 是本方法存在的理由：强制预算只能由管理端移动。
func (s *adminServiceImpl) AdminSetAPIKeyBudgetPolicy(ctx context.Context, keyID int64, policy APIKeyBudgetPolicy) (APIKeyBudgetPolicy, error) {
	repo, err := s.budgetRepository()
	if err != nil {
		return APIKeyBudgetPolicy{}, err
	}
	if _, err := s.apiKeyRepo.GetByID(ctx, keyID); err != nil {
		return APIKeyBudgetPolicy{}, err
	}
	return SaveAPIKeyBudgetPolicy(ctx, repo, keyID, policy, true)
}

// AdminAdjustAPIKeyBudgetUsage 记录一次带原因的已消费金额调整。
func (s *adminServiceImpl) AdminAdjustAPIKeyBudgetUsage(ctx context.Context, cmd *KeyBudgetAdjustmentCommand) (*APIKeyBudgetState, error) {
	repo, err := s.budgetRepository()
	if err != nil {
		return nil, err
	}
	if _, err := s.apiKeyRepo.GetByID(ctx, cmd.APIKeyID); err != nil {
		return nil, err
	}
	return AdjustAPIKeyBudgetUsage(ctx, repo, cmd)
}
