package service

import (
	"context"
	"time"

	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/pagination"
)

const DefaultProxyGroupBindingTTLSeconds = 86400

var (
	ErrProxyGroupNotFound    = infraerrors.NotFound("PROXY_GROUP_NOT_FOUND", "proxy group not found")
	ErrProxyGroupUnavailable = infraerrors.Conflict("PROXY_GROUP_UNAVAILABLE", "proxy group unavailable")
	ErrProxyBindingConflict  = infraerrors.Conflict("PROXY_BINDING_CONFLICT", "proxy binding conflict")
	ErrProxyGroupInUse       = infraerrors.Conflict("PROXY_GROUP_IN_USE", "proxy group is in use by accounts")
)

// ProxyGroup is persisted configuration; it never replaces Account.ProxyID.
type ProxyGroup struct {
	ID                int64
	WorkspaceID       int64
	Name              string
	Description       string
	Region            string
	TargetServices    []string
	Status            string
	FallbackMode      string
	FallbackProxyID   *int64
	BindingTTLSeconds int
	CreatedAt         time.Time
	UpdatedAt         time.Time
	DeletedAt         *time.Time
	Members           []ProxyGroupMember
}

type ProxyGroupMember struct {
	ID          int64
	WorkspaceID int64
	GroupID     int64
	ProxyID     int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Proxy       *Proxy
}

type ProxyGroupRepository interface {
	Create(context.Context, *ProxyGroup) error
	GetByID(context.Context, int64) (*ProxyGroup, error)
	List(context.Context, pagination.PaginationParams, string, string, string) ([]ProxyGroup, *pagination.PaginationResult, error) // status, targetService, search
	Update(context.Context, *ProxyGroup) error
	Delete(context.Context, int64) error
	ReplaceMembers(context.Context, int64, []int64) error
	ListMembers(context.Context, int64) ([]ProxyGroupMember, error)
	CountAccountsByGroupID(context.Context, int64) (int64, error)
	ListAccountSummariesByGroupID(context.Context, int64) ([]ProxyAccountSummary, error)
}

// ProxyCooldown contains operational metadata only, never proxy credentials.
type ProxyCooldown struct {
	ProxyID    int64     `json:"proxy_id"`
	Reason     string    `json:"reason"`
	ObservedAt time.Time `json:"observed_at"`
	ExpiresAt  time.Time `json:"expires_at"`
}

type ProxyGroupCache interface {
	GetBinding(ctx context.Context, groupID, accountID int64) (int64, error)
	GetBindingTTL(ctx context.Context, groupID, accountID int64) (time.Duration, error)
	TrySetBindingNX(ctx context.Context, groupID, accountID, proxyID int64, ttl time.Duration) (int64, error)
	TouchBindingIfValue(ctx context.Context, groupID, accountID, proxyID int64, ttl time.Duration) (bool, error)
	DeleteBindingIfValue(ctx context.Context, groupID, accountID, proxyID int64) (bool, error)
	SetCooldownIfAbsent(ctx context.Context, cooldown ProxyCooldown, ttl time.Duration) (bool, error)
	GetCooldown(ctx context.Context, proxyID int64) (*ProxyCooldown, error)
	ClearCooldown(ctx context.Context, proxyID int64) error
	ListCooled(ctx context.Context) ([]ProxyCooldown, error)
}
