//go:build integration

package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type SettingRepoSuite struct {
	suite.Suite
	ctx  context.Context
	repo *settingRepository
}

func (s *SettingRepoSuite) SetupTest() {
	s.ctx = context.Background()
	tx := testEntTx(s.T())
	s.repo = NewSettingRepository(tx.Client()).(*settingRepository)
}

func TestSettingRepoSuite(t *testing.T) {
	suite.Run(t, new(SettingRepoSuite))
}

func TestSettingOverrideResetRejectsStaleSnapshot(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	repo := NewSettingRepository(testEntClient(t)).(*settingRepository)
	prefix := "test_reset_" + uuid.NewString() + "_"
	keys := []string{prefix + "enabled", prefix + "client_id", prefix + "client_secret"}
	neighbor := prefix + "unrelated"
	t.Cleanup(func() {
		for _, key := range append(keys, neighbor) {
			require.NoError(t, repo.Delete(context.Background(), key))
		}
	})
	require.NoError(t, repo.SetMultiple(ctx, map[string]string{
		keys[0]: "true", keys[1]: "provider-id", keys[2]: "provider-secret", neighbor: "before",
	}))
	_, version, err := repo.GetAllVersioned(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, version)
	require.NoError(t, repo.Set(ctx, neighbor, "after"))
	require.ErrorIs(t, repo.ResetOverrides(ctx, keys, version), service.ErrSettingVersionConflict)
	values, current, err := repo.GetAllVersioned(ctx)
	require.NoError(t, err)
	require.Equal(t, "true", values[keys[0]])
	require.Equal(t, "provider-id", values[keys[1]])
	require.Equal(t, "provider-secret", values[keys[2]])
	require.Equal(t, "after", values[neighbor])
	require.NotEqual(t, version, current)

	require.NoError(t, repo.ResetOverrides(ctx, keys, current))
	values, resetVersion, err := repo.GetAllVersioned(ctx)
	require.NoError(t, err)
	for _, key := range keys {
		require.NotContains(t, values, key, "a provider reset must remove the entire requested override group")
	}
	require.Equal(t, "after", values[neighbor], "reset must preserve unrelated settings")
	require.NotEqual(t, current, resetVersion)
	require.ErrorIs(t, repo.ResetOverrides(ctx, keys, current), service.ErrSettingVersionConflict)
}

func TestSettingOverrideResetConcurrentWriters(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	repo := NewSettingRepository(testEntClient(t)).(*settingRepository)
	prefix := "test_reset_concurrent_" + uuid.NewString() + "_"
	keys := []string{prefix + "client_id", prefix + "client_secret"}
	t.Cleanup(func() {
		for _, key := range keys {
			require.NoError(t, repo.Delete(context.Background(), key))
		}
	})
	require.NoError(t, repo.SetMultiple(ctx, map[string]string{keys[0]: "provider-id", keys[1]: "provider-secret"}))
	_, version, err := repo.GetAllVersioned(ctx)
	require.NoError(t, err)
	start := make(chan struct{})
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			results <- repo.ResetOverrides(ctx, keys, version)
		}()
	}
	close(start)
	var successes, conflicts int
	for i := 0; i < 2; i++ {
		err := <-results
		switch {
		case err == nil:
			successes++
		case errors.Is(err, service.ErrSettingVersionConflict):
			conflicts++
		default:
			require.NoError(t, err)
		}
	}
	require.Equal(t, 1, successes, "only one administrator may apply the same reset snapshot")
	require.Equal(t, 1, conflicts)
	values, _, err := repo.GetAllVersioned(ctx)
	require.NoError(t, err)
	for _, key := range keys {
		require.NotContains(t, values, key)
	}
}

func (s *SettingRepoSuite) TestSetAndGetValue() {
	s.Require().NoError(s.repo.Set(s.ctx, "k1", "v1"), "Set")
	got, err := s.repo.GetValue(s.ctx, "k1")
	s.Require().NoError(err, "GetValue")
	s.Require().Equal("v1", got, "GetValue mismatch")
}

func (s *SettingRepoSuite) TestSet_Upsert() {
	s.Require().NoError(s.repo.Set(s.ctx, "k1", "v1"), "Set")
	s.Require().NoError(s.repo.Set(s.ctx, "k1", "v2"), "Set upsert")
	got, err := s.repo.GetValue(s.ctx, "k1")
	s.Require().NoError(err, "GetValue after upsert")
	s.Require().Equal("v2", got, "upsert mismatch")
}

func (s *SettingRepoSuite) TestGetValue_Missing() {
	_, err := s.repo.GetValue(s.ctx, "nonexistent")
	s.Require().Error(err, "expected error for missing key")
	s.Require().ErrorIs(err, service.ErrSettingNotFound)
}

func (s *SettingRepoSuite) TestSetMultiple_AndGetMultiple() {
	s.Require().NoError(s.repo.SetMultiple(s.ctx, map[string]string{"k2": "v2", "k3": "v3"}), "SetMultiple")
	m, err := s.repo.GetMultiple(s.ctx, []string{"k2", "k3"})
	s.Require().NoError(err, "GetMultiple")
	s.Require().Equal("v2", m["k2"])
	s.Require().Equal("v3", m["k3"])
}

func (s *SettingRepoSuite) TestGetMultiple_EmptyKeys() {
	m, err := s.repo.GetMultiple(s.ctx, []string{})
	s.Require().NoError(err, "GetMultiple with empty keys")
	s.Require().Empty(m, "expected empty map")
}

func (s *SettingRepoSuite) TestGetMultiple_Subset() {
	s.Require().NoError(s.repo.SetMultiple(s.ctx, map[string]string{"a": "1", "b": "2", "c": "3"}))
	m, err := s.repo.GetMultiple(s.ctx, []string{"a", "c", "nonexistent"})
	s.Require().NoError(err, "GetMultiple subset")
	s.Require().Equal("1", m["a"])
	s.Require().Equal("3", m["c"])
	_, exists := m["nonexistent"]
	s.Require().False(exists, "nonexistent key should not be in map")
}

func (s *SettingRepoSuite) TestGetAll() {
	s.Require().NoError(s.repo.SetMultiple(s.ctx, map[string]string{"x": "1", "y": "2"}))
	all, err := s.repo.GetAll(s.ctx)
	s.Require().NoError(err, "GetAll")
	s.Require().GreaterOrEqual(len(all), 2, "expected at least 2 settings")
	s.Require().Equal("1", all["x"])
	s.Require().Equal("2", all["y"])
}

func (s *SettingRepoSuite) TestDelete() {
	s.Require().NoError(s.repo.Set(s.ctx, "todelete", "val"))
	s.Require().NoError(s.repo.Delete(s.ctx, "todelete"), "Delete")
	_, err := s.repo.GetValue(s.ctx, "todelete")
	s.Require().Error(err, "expected missing key error after Delete")
	s.Require().ErrorIs(err, service.ErrSettingNotFound)
}

func (s *SettingRepoSuite) TestDelete_Idempotent() {
	// Delete a key that doesn't exist should not error
	s.Require().NoError(s.repo.Delete(s.ctx, "nonexistent_delete"), "Delete nonexistent should be idempotent")
}

func (s *SettingRepoSuite) TestSetMultiple_Upsert() {
	s.Require().NoError(s.repo.Set(s.ctx, "upsert_key", "old_value"))
	s.Require().NoError(s.repo.SetMultiple(s.ctx, map[string]string{"upsert_key": "new_value", "new_key": "new_val"}))

	got, err := s.repo.GetValue(s.ctx, "upsert_key")
	s.Require().NoError(err)
	s.Require().Equal("new_value", got, "SetMultiple should upsert existing key")

	got2, err := s.repo.GetValue(s.ctx, "new_key")
	s.Require().NoError(err)
	s.Require().Equal("new_val", got2)
}

// TestSet_EmptyValue 测试保存空字符串值
// 这是一个回归测试，确保可选设置（如站点Logo、API端点地址等）可以保存为空字符串
func (s *SettingRepoSuite) TestSet_EmptyValue() {
	// 测试 Set 方法保存空值
	s.Require().NoError(s.repo.Set(s.ctx, "empty_key", ""), "Set with empty value should succeed")

	got, err := s.repo.GetValue(s.ctx, "empty_key")
	s.Require().NoError(err, "GetValue for empty value")
	s.Require().Equal("", got, "empty value should be preserved")
}

// TestSetMultiple_WithEmptyValues 测试批量保存包含空字符串的设置
// 模拟用户保存站点设置时部分字段为空的场景
func (s *SettingRepoSuite) TestSetMultiple_WithEmptyValues() {
	// 模拟保存站点设置，部分字段有值，部分字段为空
	settings := map[string]string{
		"site_name":     "Sub2api",
		"site_subtitle": "Subscription to API",
		"site_logo":     "", // 用户未上传Logo
		"api_base_url":  "", // 用户未设置API地址
		"contact_info":  "", // 用户未设置联系方式
		"doc_url":       "", // 用户未设置文档链接
	}

	s.Require().NoError(s.repo.SetMultiple(s.ctx, settings), "SetMultiple with empty values should succeed")

	// 验证所有值都正确保存
	result, err := s.repo.GetMultiple(s.ctx, []string{"site_name", "site_subtitle", "site_logo", "api_base_url", "contact_info", "doc_url"})
	s.Require().NoError(err, "GetMultiple after SetMultiple with empty values")

	s.Require().Equal("Sub2api", result["site_name"])
	s.Require().Equal("Subscription to API", result["site_subtitle"])
	s.Require().Equal("", result["site_logo"], "empty site_logo should be preserved")
	s.Require().Equal("", result["api_base_url"], "empty api_base_url should be preserved")
	s.Require().Equal("", result["contact_info"], "empty contact_info should be preserved")
	s.Require().Equal("", result["doc_url"], "empty doc_url should be preserved")
}

// TestSetMultiple_UpdateToEmpty 测试将已有值更新为空字符串
// 确保用户可以清空之前设置的值
func (s *SettingRepoSuite) TestSetMultiple_UpdateToEmpty() {
	// 先设置非空值
	s.Require().NoError(s.repo.Set(s.ctx, "clearable_key", "initial_value"))

	got, err := s.repo.GetValue(s.ctx, "clearable_key")
	s.Require().NoError(err)
	s.Require().Equal("initial_value", got)

	// 更新为空值
	s.Require().NoError(s.repo.SetMultiple(s.ctx, map[string]string{"clearable_key": ""}), "Update to empty should succeed")

	got, err = s.repo.GetValue(s.ctx, "clearable_key")
	s.Require().NoError(err)
	s.Require().Equal("", got, "value should be updated to empty string")
}
