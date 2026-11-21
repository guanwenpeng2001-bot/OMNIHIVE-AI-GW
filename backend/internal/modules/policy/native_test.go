package policy

import (
	"encoding/json"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/routing/snapshot"
	"github.com/stretchr/testify/require"
)

func TestNativeRequestOnlyPolicyRequiresVerifiedEndpoint(t *testing.T) {
	for _, tc := range []struct {
		name        string
		restriction Constraints
		want        map[int64]bool
	}{
		{"region", Constraints{Regions: &List{"allow_list", []string{"EU"}}}, map[int64]bool{1: true, 3: true}},
		{"retention", Constraints{Retention: "strict_zero"}, map[int64]bool{1: true, 2: true}},
		{"training", Constraints{Training: "prohibited"}, map[int64]bool{1: true, 2: true}},
		{"combined", Constraints{Regions: &List{"allow_list", []string{"EU"}}, Retention: "strict_zero", Training: "prohibited"}, map[int64]bool{1: true}},
		{"denied", Constraints{Regions: &List{"allow_list", []string{"AP"}}}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			graph := Graph{Facts: map[string]Evidence{}, Snapshot: snapshot.CandidateSnapshot{Candidates: []snapshot.Candidate{
				{ID: "eu", ModelID: "model", Region: "EU", RegionVerified: true, ZeroRetention: true, RetentionVerified: true},
				{ID: "us", ModelID: "model", Region: "US", RegionVerified: true, ZeroRetention: true, RetentionVerified: true},
				{ID: "retention-unverified", ModelID: "model", Region: "EU", RegionVerified: true, ZeroRetention: true},
				{ID: "stale", ModelID: "model", Region: "EU", RegionVerified: true, ZeroRetention: true, RetentionVerified: true},
			}}}
			for i, c := range graph.Snapshot.Candidates {
				graph.Facts[c.ID] = Evidence{Connection: &ConnectionFence{int64(i + 1), "v1"}, VerifiedNoTraining: i != 2}
			}
			raw, err := json.Marshal(graph)
			require.NoError(t, err)
			mock.ExpectBegin()
			mock.ExpectQuery("SELECT v.id,v.definition").WillReturnRows(sqlmock.NewRows([]string{"id", "definition"}))
			mock.ExpectQuery("SELECT EXISTS").WillReturnRows(sqlmock.NewRows([]string{"allowed"}).AddRow(true))
			mock.ExpectQuery("SELECT graph FROM routing_candidate_snapshots").WithArgs(1, 2, 3, 4, 5, "model").WillReturnRows(sqlmock.NewRows([]string{"graph"}).AddRow(raw))
			rows := sqlmock.NewRows([]string{"id", "provider", "verified", "pools"})
			for id := 1; id <= 5; id++ {
				verification := "v1"
				if id == 4 {
					verification = "v2"
				}
				if id == 5 {
					verification = ""
				}
				rows.AddRow(id, "provider", verification, "{}")
			}
			mock.ExpectQuery("SELECT a.id,coalesce").WillReturnRows(rows)
			if tc.want == nil {
				mock.ExpectRollback()
			} else {
				mock.ExpectCommit()
			}
			req := Layer{"request-v1", Definition{Name: "test", Scope: Scope{Type: "request", ID: "request"}, Constraints: tc.restriction}}
			accounts, err := NewStore(db, nil).NativeRequestAccounts(t.Context(), scope.Authorized{WorkspaceID: 1, ProjectID: 2, ActorID: 3, KeyID: 4, Version: 5}, "model", &req)
			if tc.want == nil {
				require.ErrorIs(t, err, NativePolicyDenied)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.want, accounts)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
