//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// captureUpdateAPIKeyRepoStub records both the declared columns and the key
// snapshot the service wanted to persist.
type captureUpdateAPIKeyRepoStub struct {
	updateFieldsAPIKeyRepoStub
	persisted *APIKey
}

func (s *captureUpdateAPIKeyRepoStub) Update(ctx context.Context, key *APIKey, fields APIKeyUpdateFields) error {
	s.persisted = key
	return s.updateFieldsAPIKeyRepoStub.Update(ctx, key, fields)
}

func newDisabledReviveTestService(key *APIKey) (*APIKeyService, *captureUpdateAPIKeyRepoStub) {
	repo := &captureUpdateAPIKeyRepoStub{updateFieldsAPIKeyRepoStub: updateFieldsAPIKeyRepoStub{key: key}}
	return &APIKeyService{apiKeyRepo: repo}, repo
}

func int64Pointer(v int64) *int64 { return &v }

// 人工停用（管理员或风控把 status 设为 disabled）只能由管理侧解除。
// 面板编辑请求里带 status=active 时必须显式失败，而不是把凭据悄悄复活。
func TestAPIKeyUpdate_RejectsOwnerRevivingDisabledKey(t *testing.T) {
	active := StatusActive
	name := "renamed"
	svc, repo := newDisabledReviveTestService(&APIKey{
		ID: 1, UserID: 7, Key: "sk-test", Name: "before", Status: StatusAPIKeyDisabled, GroupID: int64Pointer(3),
	})

	_, err := svc.Update(context.Background(), 1, 7, UpdateAPIKeyRequest{Name: &name, Status: &active})

	require.ErrorIs(t, err, ErrAPIKeyDisabledByAdmin)
	require.Empty(t, repo.updateFields)
	require.Nil(t, repo.persisted)
}

// 编辑其它字段（含扩额、延长有效期）不会顺带把 disabled 改回 active：
// 那两个自动复活分支只对 quota_exhausted / expired 生效。
func TestAPIKeyUpdate_DisabledKeyStaysDisabledThroughQuotaAndExpiryEdits(t *testing.T) {
	quota := 500.0
	expiresAt := time.Now().Add(24 * time.Hour)

	svc, repo := newDisabledReviveTestService(&APIKey{
		ID: 1, UserID: 7, Key: "sk-test", Status: StatusAPIKeyDisabled, Quota: 100, QuotaUsed: 100,
	})

	_, err := svc.Update(context.Background(), 1, 7, UpdateAPIKeyRequest{Quota: &quota, ExpiresAt: &expiresAt})
	require.NoError(t, err)

	require.Equal(t, []APIKeyUpdateFields{{Quota: true, ExpiresAt: true}}, repo.updateFields)
	require.Equal(t, StatusAPIKeyDisabled, repo.persisted.Status)
}

// 管理员停用的凭据在面板里重新提交它当前的状态也不是复活路径：
// binding 只接受 active/inactive，Service 层再兜一次，任何非 disabled 的写入都被拒绝。
func TestAPIKeyUpdate_DisabledKeyRejectsInactiveStatusRewrite(t *testing.T) {
	inactive := "inactive"
	svc, repo := newDisabledReviveTestService(&APIKey{
		ID: 1, UserID: 7, Key: "sk-test", Status: StatusAPIKeyDisabled,
	})

	_, err := svc.Update(context.Background(), 1, 7, UpdateAPIKeyRequest{Status: &inactive})

	require.ErrorIs(t, err, ErrAPIKeyDisabledByAdmin)
	require.Empty(t, repo.updateFields)
}

// 额度耗尽/过期是限额状态，仍可由 owner 编辑恢复（T2-2.9 的正例：恢复后 isValid 为真）。
func TestAPIKeyUpdate_QuotaExhaustedKeyIsReactivatedByQuotaIncrease(t *testing.T) {
	quota := 500.0
	svc, repo := newDisabledReviveTestService(&APIKey{
		ID: 1, UserID: 7, Key: "sk-test", Status: StatusAPIKeyQuotaExhausted, Quota: 100, QuotaUsed: 100,
	})

	_, err := svc.Update(context.Background(), 1, 7, UpdateAPIKeyRequest{Quota: &quota})
	require.NoError(t, err)

	require.Equal(t, []APIKeyUpdateFields{{Quota: true, Status: true}}, repo.updateFields)
	require.Equal(t, StatusActive, repo.persisted.Status)
}

// 只改设置（改名/限额）不得改派生态：分组归属列不写入，Key 仍留在原分组。
func TestAPIKeyUpdate_KeepsGroupAttributionWhenEditingSettings(t *testing.T) {
	name := "renamed"
	quota := 200.0
	svc, repo := newDisabledReviveTestService(&APIKey{
		ID: 1, UserID: 7, Key: "sk-test", Name: "before", Status: StatusActive, GroupID: int64Pointer(3), Group: &Group{ID: 3, Status: StatusActive},
	})

	_, err := svc.Update(context.Background(), 1, 7, UpdateAPIKeyRequest{Name: &name, Quota: &quota})
	require.NoError(t, err)

	require.False(t, repo.updateFields[0].GroupID)
	require.NotNil(t, repo.persisted.GroupID)
	require.Equal(t, int64(3), *repo.persisted.GroupID)
}
