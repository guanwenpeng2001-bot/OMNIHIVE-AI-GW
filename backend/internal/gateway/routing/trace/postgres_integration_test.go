//go:build integration

package trace

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/journal"
	_ "github.com/lib/pq"
)

func TestPostgresRoutingDecisions(t *testing.T) {
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	if dsn == "" {
		t.Skip("set UPSTREAM_JOB_TEST_DSN to a dedicated test database")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `
CREATE TEMP TABLE gateway_executions (
    request_id UUID PRIMARY KEY, workspace_id BIGINT NOT NULL, project_id BIGINT NOT NULL
)`); err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "migrations", "297_routing_decisions.sql"))
	if err != nil {
		t.Fatal(err)
	}
	// PostgreSQL requires both relations of a temporary foreign key to be temporary.
	sqlText := "CREATE TEMP " + string(migration[len("CREATE "):])
	if _, err := db.ExecContext(ctx, sqlText); err != nil {
		t.Fatal(err)
	}

	e := journal.AdmittedExecution{RequestID: journal.NewID(), Scope: scope.Authorized{WorkspaceID: 1, ProjectID: 2}}
	if _, err := db.ExecContext(ctx, "INSERT INTO gateway_executions VALUES ($1, $2, $3)", e.RequestID, e.Scope.WorkspaceID, e.Scope.ProjectID); err != nil {
		t.Fatal(err)
	}
	store := NewPostgres(db)
	decision := DecisionTrace{Mode: Simulation, SnapshotID: journal.NewID(), Candidates: []Candidate{{ID: "1234", State: "eligible"}},
		Selected: &Selection{CandidateID: "1234"}}
	if err := store.Record(ctx, e, decision); err != nil {
		t.Fatal(err)
	}
	decision.Mode = Execution
	wrongScope := e
	wrongScope.Scope.ProjectID++
	if err := store.Record(ctx, wrongScope, decision); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := store.Record(ctx, e, decision); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM gateway_routing_decisions").Scan(&count); err != nil || count != 1 {
		t.Fatalf("expected one scoped execution trace, got %d: %v", count, err)
	}
	var id, selected string
	if err := db.QueryRowContext(ctx, "SELECT decision#>>'{candidates,0,id}', decision#>>'{selected,candidate_id}' FROM gateway_routing_decisions").Scan(&id, &selected); err != nil {
		t.Fatal(err)
	}
	if id != "candidate-1" || selected != id || decision.Candidates[0].ID != "1234" || decision.Selected.CandidateID != "1234" {
		t.Fatalf("incorrect trace redaction: %q / %q", id, selected)
	}
}
