package service

import (
	"context"
	"errors"
	"time"
)

var ErrMCPGrantNotFound = errors.New("mcp grant not found")

type MCPGrantDTO struct {
	ID          string     `json:"id"`
	ClientID    string     `json:"client_id"`
	Name        string     `json:"name"`
	RedirectURI string     `json:"redirect_uri"`
	Scope       string     `json:"scope"`
	Permission  string     `json:"permission"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   time.Time  `json:"expires_at"`
	LastUsedAt  *time.Time `json:"last_used_at"`
}

type MCPGrantStore interface {
	List(context.Context, int64) ([]MCPGrantDTO, error)
	Create(context.Context, int64, MCPGrantDTO) (MCPGrantDTO, error)
	Revoke(context.Context, int64, string) (MCPGrantDTO, error)
	Active(context.Context, int64, string) (MCPGrantDTO, bool, error)
	RegisterClient(context.Context, int64, string, string, string) error
	ClientMatches(context.Context, int64, string, string) (bool, error)
}
