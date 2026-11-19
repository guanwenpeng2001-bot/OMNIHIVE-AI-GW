//go:build integration

package testfixture

import (
	"database/sql"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func Database(t *testing.T, env, expected string) (*sql.DB, *config.Config) {
	t.Helper()
	dsn := os.Getenv(env)
	if dsn == "" {
		t.Skip("requires " + env)
	}
	u, e := url.Parse(dsn)
	require.NoError(t, e)
	require.Contains(t, []string{"postgres", "postgresql"}, u.Scheme)
	require.Equal(t, "/"+expected, u.Path)
	require.Equal(t, "sslmode=disable", u.RawQuery)
	require.Empty(t, u.Fragment)
	require.Contains(t, []string{"localhost", "127.0.0.1"}, u.Hostname())
	require.NotNil(t, u.User)
	_, p, e := net.SplitHostPort(u.Host)
	require.NoError(t, e)
	port, e := strconv.Atoi(p)
	require.NoError(t, e)
	for _, k := range []string{"PGHOSTADDR", "PGSERVICE", "PGSERVICEFILE", "PGOPTIONS"} {
		require.Empty(t, os.Getenv(k))
	}
	maintenance, e := sql.Open("postgres", dsn)
	require.NoError(t, e)
	t.Cleanup(func() { maintenance.Close() })
	var actual string
	require.NoError(t, maintenance.QueryRow("SELECT current_database()").Scan(&actual))
	require.Equal(t, expected, actual)
	name := "be09_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, e = maintenance.Exec("CREATE DATABASE " + name + " TEMPLATE template0")
	require.NoError(t, e)
	t.Cleanup(func() { _, e := maintenance.Exec("DROP DATABASE " + name + " WITH (FORCE)"); require.NoError(t, e) })
	u.Path = "/" + name
	db, e := sql.Open("postgres", u.String())
	require.NoError(t, e)
	t.Cleanup(func() { db.Close() })
	pass, _ := u.User.Password()
	return db, &config.Config{Database: config.DatabaseConfig{Host: u.Hostname(), Port: port, DBName: name, User: u.User.Username(), Password: pass, SSLMode: "disable"}}
}
