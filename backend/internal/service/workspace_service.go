package service

import (
	"context"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var ErrWorkspaceNotFound = infraerrors.NotFound("WORKSPACE_NOT_FOUND", "workspace not found")

type Workspace struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	IsDefault bool      `json:"is_default"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

type WorkspaceMember struct {
	UserID int64  `json:"user_id"`
	Role   string `json:"role"`
}

type WorkspaceRepository interface {
	List(context.Context, int64, bool) ([]Workspace, error)
	Create(context.Context, string) (*Workspace, error)
	Role(context.Context, int64, int64, bool) (string, error)
	Members(context.Context, int64) ([]WorkspaceMember, error)
	SetMember(context.Context, int64, int64, string) error
	RemoveMember(context.Context, int64, int64) error
	ContainsResources(context.Context, int64, string, []int64) (bool, error)
}

type WorkspaceService struct{ repository WorkspaceRepository }

func NewWorkspaceService(repository WorkspaceRepository) *WorkspaceService {
	return &WorkspaceService{repository: repository}
}

func (s *WorkspaceService) List(ctx context.Context, userID int64, globalAdmin bool) ([]Workspace, error) {
	return s.repository.List(ctx, userID, globalAdmin)
}

func (s *WorkspaceService) Create(ctx context.Context, name string, globalAdmin bool) (*Workspace, error) {
	if !globalAdmin {
		return nil, ErrWorkspaceNotFound
	}
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 100 {
		return nil, infraerrors.BadRequest("INVALID_WORKSPACE_NAME", "workspace name must contain 1 to 100 characters")
	}
	return s.repository.Create(ctx, name)
}

// Authorize performs a fresh membership lookup, including for read operations.
func (s *WorkspaceService) Authorize(ctx context.Context, workspaceID, userID int64, globalAdmin, requireAdmin bool) error {
	if workspaceID <= 0 || userID <= 0 {
		return ErrWorkspaceNotFound
	}
	role, err := s.repository.Role(ctx, workspaceID, userID, globalAdmin)
	if err != nil {
		return err
	}
	if role != "admin" && (requireAdmin || role != "member") {
		return ErrWorkspaceNotFound
	}
	return nil
}

func (s *WorkspaceService) Members(ctx context.Context, workspaceID int64) ([]WorkspaceMember, error) {
	return s.repository.Members(ctx, workspaceID)
}

func (s *WorkspaceService) SetMember(ctx context.Context, workspaceID, userID int64, role string) error {
	if userID <= 0 {
		return ErrWorkspaceNotFound
	}
	if role != "member" && role != "admin" {
		return infraerrors.BadRequest("INVALID_WORKSPACE_ROLE", "role must be member or admin")
	}
	return s.repository.SetMember(ctx, workspaceID, userID, role)
}

func (s *WorkspaceService) RemoveMember(ctx context.Context, workspaceID, userID int64) error {
	return s.repository.RemoveMember(ctx, workspaceID, userID)
}

func (s *WorkspaceService) RequireResources(ctx context.Context, workspaceID int64, resource string, identifiers []int64) error {
	if len(identifiers) == 0 {
		return nil
	}
	contained, err := s.repository.ContainsResources(ctx, workspaceID, resource, identifiers)
	if err != nil {
		return err
	}
	if !contained {
		return ErrWorkspaceNotFound
	}
	return nil
}
