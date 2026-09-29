//go:build unit

package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type geminiAccountIsolationCache struct {
	GeminiTokenCache
	tokens map[string]string
}

func (c *geminiAccountIsolationCache) GetAccessToken(_ context.Context, key string) (string, error) {
	return c.tokens[key], nil
}

func (c *geminiAccountIsolationCache) SetAccessToken(_ context.Context, key, token string, _ time.Duration) error {
	c.tokens[key] = token
	return nil
}

func TestGeminiTokenProvider_SharedProjectKeepsCredentialOwnersIsolated(t *testing.T) {
	cache := &geminiAccountIsolationCache{tokens: map[string]string{"gemini:shared-project": "legacy-shared-token"}}
	provider := NewGeminiTokenProvider(nil, cache, nil)
	account := func(id int64, token string) *Account {
		return &Account{ID: id, Platform: PlatformGemini, Type: AccountTypeOAuth, Credentials: map[string]any{
			"project_id": "shared-project", "access_token": token,
			"expires_at": time.Now().Add(time.Hour).Format(time.RFC3339),
		}}
	}
	a, b := account(201, "token-a"), account(202, "token-b")
	for _, acc := range []*Account{a, b, a, b} {
		token, err := provider.GetAccessToken(context.Background(), acc)
		require.NoError(t, err)
		require.Equal(t, acc.GetCredential("access_token"), token)
	}
	require.NotEqual(t, GeminiTokenCacheKey(a), GeminiTokenCacheKey(b))
	oldKey := GeminiTokenCacheKey(a)
	a.Credentials["project_id"] = "auto-detected-project"
	require.NotEqual(t, oldKey, GeminiTokenCacheKey(a), "routing identity changes must retire cached tokens")
	token, err := provider.GetAccessToken(context.Background(), a)
	require.NoError(t, err)
	require.Equal(t, "token-a", token)
}

func TestGeminiTokenCacheKey(t *testing.T) {
	tests := []struct {
		name     string
		account  *Account
		expected string
	}{
		{
			name: "with_project_id",
			account: &Account{
				ID: 100,
				Credentials: map[string]any{
					"project_id": "my-project-123",
				},
			},
			expected: "gemini:account:100",
		},
		{
			name: "project_id_with_whitespace",
			account: &Account{
				ID: 101,
				Credentials: map[string]any{
					"project_id": "  project-with-spaces  ",
				},
			},
			expected: "gemini:account:101",
		},
		{
			name: "empty_project_id_fallback_to_account_id",
			account: &Account{
				ID: 102,
				Credentials: map[string]any{
					"project_id": "",
				},
			},
			expected: "gemini:account:102",
		},
		{
			name: "whitespace_only_project_id_fallback_to_account_id",
			account: &Account{
				ID: 103,
				Credentials: map[string]any{
					"project_id": "   ",
				},
			},
			expected: "gemini:account:103",
		},
		{
			name: "no_project_id_key_fallback_to_account_id",
			account: &Account{
				ID:          104,
				Credentials: map[string]any{},
			},
			expected: "gemini:account:104",
		},
		{
			name: "nil_credentials_fallback_to_account_id",
			account: &Account{
				ID:          105,
				Credentials: nil,
			},
			expected: "gemini:account:105",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GeminiTokenCacheKey(tt.account)
			if strings.HasPrefix(tt.expected, "ag:") || tt.account == nil {
				require.Equal(t, tt.expected, result)
			} else {
				require.True(t, strings.HasPrefix(result, tt.expected+":"))
				require.Len(t, strings.TrimPrefix(result, tt.expected+":"), 64)
			}
		})
	}
}

func TestAntigravityTokenCacheKey(t *testing.T) {
	tests := []struct {
		name     string
		account  *Account
		expected string
	}{
		{
			name: "with_project_id",
			account: &Account{
				ID: 200,
				Credentials: map[string]any{
					"project_id": "ag-project-456",
				},
			},
			expected: "ag:account:200",
		},
		{
			name: "project_id_with_whitespace",
			account: &Account{
				ID: 201,
				Credentials: map[string]any{
					"project_id": "  ag-project-spaces  ",
				},
			},
			expected: "ag:account:201",
		},
		{
			name: "empty_project_id_fallback_to_account_id",
			account: &Account{
				ID: 202,
				Credentials: map[string]any{
					"project_id": "",
				},
			},
			expected: "ag:account:202",
		},
		{
			name: "whitespace_only_project_id_fallback_to_account_id",
			account: &Account{
				ID: 203,
				Credentials: map[string]any{
					"project_id": "   ",
				},
			},
			expected: "ag:account:203",
		},
		{
			name: "no_project_id_key_fallback_to_account_id",
			account: &Account{
				ID:          204,
				Credentials: map[string]any{},
			},
			expected: "ag:account:204",
		},
		{
			name: "nil_credentials_fallback_to_account_id",
			account: &Account{
				ID:          205,
				Credentials: nil,
			},
			expected: "ag:account:205",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := AntigravityTokenCacheKey(tt.account)
			if strings.HasPrefix(tt.expected, "ag:") || tt.account == nil {
				require.Equal(t, tt.expected, result)
			} else {
				require.True(t, strings.HasPrefix(result, tt.expected+":"))
				require.Len(t, strings.TrimPrefix(result, tt.expected+":"), 64)
			}
		})
	}
}

func TestOpenAITokenCacheKey(t *testing.T) {
	tests := []struct {
		name     string
		account  *Account
		expected string
	}{
		{
			name: "basic_account",
			account: &Account{
				ID: 300,
			},
			expected: "openai:account:300",
		},
		{
			name: "account_with_credentials",
			account: &Account{
				ID: 301,
				Credentials: map[string]any{
					"access_token": "test-token",
				},
			},
			expected: "openai:account:301",
		},
		{
			name: "account_id_zero",
			account: &Account{
				ID: 0,
			},
			expected: "openai:account:0",
		},
		{
			name: "large_account_id",
			account: &Account{
				ID: 9999999999,
			},
			expected: "openai:account:9999999999",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := OpenAITokenCacheKey(tt.account)
			if strings.HasPrefix(tt.expected, "ag:") || tt.account == nil {
				require.Equal(t, tt.expected, result)
			} else {
				require.True(t, strings.HasPrefix(result, tt.expected+":"))
				require.Len(t, strings.TrimPrefix(result, tt.expected+":"), 64)
			}
		})
	}
}

func TestGrokTokenCacheKey(t *testing.T) {
	tests := []struct {
		name     string
		account  *Account
		expected string
	}{
		{
			name: "basic_account",
			account: &Account{
				ID: 350,
			},
			expected: "grok:account:350",
		},
		{
			name: "account_with_email_uses_account_id",
			account: &Account{
				ID: 351,
				Credentials: map[string]any{
					"email": "same-user@example.com",
				},
			},
			expected: "grok:account:351",
		},
		{
			name: "account_id_zero",
			account: &Account{
				ID: 0,
			},
			expected: "grok:account:0",
		},
		{
			name:     "nil_account",
			account:  nil,
			expected: "grok:account:0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GrokTokenCacheKey(tt.account)
			if strings.HasPrefix(tt.expected, "ag:") || tt.account == nil {
				require.Equal(t, tt.expected, result)
			} else {
				require.True(t, strings.HasPrefix(result, tt.expected+":"))
				require.Len(t, strings.TrimPrefix(result, tt.expected+":"), 64)
			}
		})
	}
}

func TestGrokTokenCacheKeySeparatesAccountsWithSameEmail(t *testing.T) {
	first := &Account{
		ID: 351,
		Credentials: map[string]any{
			"email": "same-user@example.com",
		},
	}
	second := &Account{
		ID: 352,
		Credentials: map[string]any{
			"email": "same-user@example.com",
		},
	}

	require.NotEqual(t, GrokTokenCacheKey(first), GrokTokenCacheKey(second))
}

func TestClaudeTokenCacheKey(t *testing.T) {
	tests := []struct {
		name     string
		account  *Account
		expected string
	}{
		{
			name: "basic_account",
			account: &Account{
				ID: 400,
			},
			expected: "claude:account:400",
		},
		{
			name: "account_with_credentials",
			account: &Account{
				ID: 401,
				Credentials: map[string]any{
					"access_token": "claude-token",
				},
			},
			expected: "claude:account:401",
		},
		{
			name: "account_id_zero",
			account: &Account{
				ID: 0,
			},
			expected: "claude:account:0",
		},
		{
			name: "large_account_id",
			account: &Account{
				ID: 9999999999,
			},
			expected: "claude:account:9999999999",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ClaudeTokenCacheKey(tt.account)
			if strings.HasPrefix(tt.expected, "ag:") || tt.account == nil {
				require.Equal(t, tt.expected, result)
			} else {
				require.True(t, strings.HasPrefix(result, tt.expected+":"))
				require.Len(t, strings.TrimPrefix(result, tt.expected+":"), 64)
			}
		})
	}
}

func TestCacheKeyUniqueness(t *testing.T) {
	// 确保不同平台的缓存键不会冲突
	account := &Account{ID: 123}

	openaiKey := OpenAITokenCacheKey(account)
	claudeKey := ClaudeTokenCacheKey(account)

	require.NotEqual(t, openaiKey, claudeKey, "OpenAI and Claude cache keys should be different")
	require.Contains(t, openaiKey, "openai:")
	require.Contains(t, claudeKey, "claude:")
}
