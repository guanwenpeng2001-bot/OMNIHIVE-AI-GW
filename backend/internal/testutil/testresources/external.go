// Package testresources owns the opt-in lifecycle for already running local test services.
package testresources

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/runtimeenv"
	_ "github.com/lib/pq"
)

func External() bool {
	return runtimeenv.Get("OMNIHIVE_TEST_EXTERNAL_SERVICES", "SUB2API_TEST_EXTERNAL_SERVICES") == "1"
}

// Run selects the lifecycle before any managed service callback can run.
func Run(external, managed func() int) int {
	if External() {
		return external()
	}
	return managed()
}

// ValidatePostgres deliberately accepts only explicit local URI connections.
// Query parameters must not override the host, port, database or search path.
func ValidatePostgres(connectionString, database string) error {
	if database != "job_test" && database != "stats_test" {
		return errors.New("test database must be job_test or stats_test")
	}
	parsed, err := url.Parse(connectionString)
	if err != nil || parsed == nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") ||
		!explicitLocalAddress(parsed.Host) || parsed.Path != "/"+database || parsed.Fragment != "" ||
		parsed.User == nil || parsed.User.Username() == "" {
		return errors.New("expected explicit local host and port with isolated test database URI")
	}
	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return errors.New("invalid test database query parameters")
	}
	for key, values := range query {
		if len(values) != 1 || (key != "sslmode" && key != "TimeZone" && key != "connect_timeout") {
			return errors.New("test database URI contains an unsupported connection override")
		}
	}
	for _, name := range []string{"PGHOSTADDR", "PGSERVICE", "PGSERVICEFILE", "PGOPTIONS"} {
		if os.Getenv(name) != "" {
			return fmt.Errorf("external tests reject %s connection overrides", name)
		}
	}
	return nil
}

func ValidateRedis(address string) error {
	if !explicitLocalAddress(address) {
		return errors.New("expected explicit local host and port for test Redis")
	}
	return nil
}

// Ports belong to the caller's isolated environment: CI services and local test
// instances use different ports. Keep routing explicit and loopback-only.
func explicitLocalAddress(address string) bool {
	host, port, err := net.SplitHostPort(address)
	if err != nil || (host != "localhost" && host != "127.0.0.1") {
		return false
	}
	number, err := strconv.ParseUint(port, 10, 16)
	return err == nil && number > 0
}

func OpenPostgres(ctx context.Context, connectionString, database string) (*sql.DB, error) {
	if err := ValidatePostgres(connectionString, database); err != nil {
		return nil, err
	}
	connection, err := sql.Open("postgres", connectionString)
	if err != nil {
		return nil, errors.New("could not open isolated PostgreSQL connection")
	}
	var actualDatabase string
	if err := connection.QueryRowContext(ctx, "SELECT current_database()").Scan(&actualDatabase); err != nil || actualDatabase != database {
		_ = connection.Close()
		return nil, errors.New("isolated PostgreSQL database identity check failed")
	}
	return connection, nil
}

// RequireDisposableDatabase rejects bad configuration before recording a real
// coverage gap. Full migrations contain public-qualified DDL: a new schema is
// not an equivalent replacement for a fresh database.
func RequireDisposableDatabase(test *testing.T, variable string) {
	test.Helper()
	if !External() {
		return
	}
	if strings.TrimSpace(os.Getenv(variable)) == "" {
		// An unset variable means this suite's disposable database was not
		// configured; that is a coverage gap to skip, not a misconfiguration to
		// fail on. A variable that is set but unusable still fails below.
		test.Skipf("external services: %s is not set; this suite needs a fresh disposable database", variable)
	}
	if err := ValidatePostgres(os.Getenv(variable), "job_test"); err != nil {
		test.Fatal(err)
	}
	test.Skip("external services: requires a fresh disposable database; database creation/deletion is forbidden")
}

// OpenSchema creates only an invocation-owned fixture namespace. Callers must
// use it only for schema-local SQL, never for the full migration chain.
func OpenSchema(test *testing.T, connectionString string) *sql.DB {
	test.Helper()
	ctx := context.Background()
	admin, err := OpenPostgres(ctx, connectionString, "job_test")
	if err != nil {
		test.Fatal(err)
	}
	schema := "test_external_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.ExecContext(ctx, `CREATE SCHEMA "`+schema+`"`); err != nil {
		_ = admin.Close()
		test.Fatal(err)
	}
	test.Cleanup(func() {
		defer func() { _ = admin.Close() }()
		if _, err := admin.ExecContext(context.Background(), `DROP SCHEMA "`+schema+`" CASCADE`); err != nil {
			test.Error(err)
		}
	})
	parsed, _ := url.Parse(connectionString)
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	connection, err := sql.Open("postgres", parsed.String())
	if err != nil {
		test.Fatal("could not open test fixture schema")
	}
	test.Cleanup(func() { _ = connection.Close() })
	return connection
}
