package repository

import (
	"testing"

	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/workspacemember"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUserRepositoryDefaultWorkspaceOnboarding(t *testing.T) {
	for _, guard := range []bool{false, true} {
		for _, role := range []string{service.RoleAdmin, service.RoleUser} {
			name := role
			if guard {
				name += "-guard"
			}
			t.Run(name, func(t *testing.T) {
				repo, client := newUserEntRepo(t)
				_, err := client.Workspace.Create().SetName("Default").SetIsDefault(true).Save(t.Context())
				require.NoError(t, err)
				u := &service.User{Email: name + "@example.test", PasswordHash: "hash", Role: role, Status: service.StatusActive}
				if guard {
					err = repo.CreateWithEmailAliasGuard(t.Context(), u)
				} else {
					err = repo.Create(t.Context(), u)
				}
				require.NoError(t, err)
				member, err := client.WorkspaceMember.Query().Where(workspacemember.UserIDEQ(u.ID), workspacemember.WorkspaceIDEQ(1)).Only(t.Context())
				require.NoError(t, err)
				want := "member"
				if role == service.RoleAdmin {
					want = "admin"
				}
				require.Equal(t, want, string(member.Role))
				require.Equal(t, 1, client.WorkspaceMember.Query().CountX(t.Context()))
			})
		}
	}
}

func TestUserRepositoryWorkspaceOnboardingRollsBack(t *testing.T) {
	repo, client := newUserEntRepo(t)
	_, err := client.Workspace.Create().SetName("Default").SetIsDefault(true).Save(t.Context())
	require.NoError(t, err)
	_, err = client.ExecContext(t.Context(), `CREATE TRIGGER reject_membership BEFORE INSERT ON workspace_members BEGIN SELECT RAISE(ABORT,'fixture rejection'); END`)
	require.NoError(t, err)
	u := &service.User{Email: "rollback@example.test", PasswordHash: "hash", Role: service.RoleUser, Status: service.StatusActive}
	require.ErrorContains(t, repo.Create(t.Context(), u), "provision default workspace membership")
	require.Zero(t, client.User.Query().CountX(t.Context()))
}

func TestUserRepositoryWorkspaceOnboardingUsesOuterTransaction(t *testing.T) {
	repo, client := newUserEntRepo(t)
	_, err := client.Workspace.Create().SetName("Default").SetIsDefault(true).Save(t.Context())
	require.NoError(t, err)
	tx, err := client.Tx(t.Context())
	require.NoError(t, err)
	u := &service.User{Email: "outer-tx@example.test", PasswordHash: "hash", Role: service.RoleUser, Status: service.StatusActive}
	require.NoError(t, repo.Create(dbent.NewTxContext(t.Context(), tx), u))
	require.Equal(t, 1, tx.Client().WorkspaceMember.Query().CountX(t.Context()))
	require.NoError(t, tx.Rollback())
	require.Zero(t, client.User.Query().CountX(t.Context()))
	require.Zero(t, client.WorkspaceMember.Query().CountX(t.Context()))
}
