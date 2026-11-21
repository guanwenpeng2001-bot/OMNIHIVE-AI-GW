//go:build integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestMCPGrantRepositorySharedRevocation(t *testing.T) {
	dsn := os.Getenv("MCP_GRANT_TEST_DSN")
	if dsn == "" {
		t.Skip("set MCP_GRANT_TEST_DSN to a disposable PostgreSQL instance")
	}
	admin, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	name := fmt.Sprintf("mcp_grants_%d", time.Now().UnixNano())
	_, err = admin.Exec(`CREATE DATABASE "` + name + `"`)
	require.NoError(t, err)
	parsed, err := url.Parse(dsn)
	require.NoError(t, err)
	parsed.Path = "/" + name
	db, err := sql.Open("postgres", parsed.String())
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = db.Close()
		_, dropErr := admin.Exec(`DROP DATABASE "` + name + `" WITH (FORCE)`)
		require.NoError(t, dropErr)
		_ = admin.Close()
	})
	require.NoError(t, ApplyMigrations(context.Background(), db))
	_, err = db.Exec(`INSERT INTO users(id,email,username,password,role,status) VALUES(1,'mcp@example.test','mcp','x','user',1)`)
	require.NoError(t, err)

	first := NewMCPGrantRepository(db)
	second := NewMCPGrantRepository(db)
	require.NoError(t, first.RegisterClient(context.Background(), 1, "client", "Client", "https://example.test/callback"))
	matches, err := second.ClientMatches(context.Background(), 1, "client", "https://example.test/callback")
	require.NoError(t, err)
	require.True(t, matches)
	matches, err = second.ClientMatches(context.Background(), 1, "client", "https://attacker.test/callback")
	require.NoError(t, err)
	require.False(t, matches)
	now := time.Now().UTC()
	grant, err := first.Create(context.Background(), 1, service.MCPGrantDTO{ID: uuid.NewString(), ClientID: "client", Name: "Client", RedirectURI: "https://example.test/callback", Scope: "admin", Permission: "write", Status: "active", CreatedAt: now, ExpiresAt: now.Add(time.Hour)})
	require.NoError(t, err)
	_, active, err := second.Active(context.Background(), 1, grant.ID)
	require.NoError(t, err)
	require.True(t, active)
	_, err = second.Revoke(context.Background(), 1, grant.ID)
	require.NoError(t, err)
	_, active, err = first.Active(context.Background(), 1, grant.ID)
	require.NoError(t, err)
	require.False(t, active)
}
