package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

type mcpGrantRepository struct{ db *sql.DB }

func NewMCPGrantRepository(db *sql.DB) service.MCPGrantStore {
	return &mcpGrantRepository{db: db}
}

func (repo *mcpGrantRepository) List(ctx context.Context, userID int64) ([]service.MCPGrantDTO, error) {
	rows, err := repo.db.QueryContext(ctx, `SELECT id,client_id,name,redirect_uri,scope,permission,status,created_at,expires_at,last_used_at FROM mcp_grants WHERE user_id=$1 ORDER BY created_at,id`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	grants := []service.MCPGrantDTO{}
	for rows.Next() {
		grant, err := scanMCPGrant(rows)
		if err != nil {
			return nil, err
		}
		grants = append(grants, grant)
	}
	return grants, rows.Err()
}

type mcpScanner interface{ Scan(...any) error }

func scanMCPGrant(scanner mcpScanner) (service.MCPGrantDTO, error) {
	var grant service.MCPGrantDTO
	err := scanner.Scan(&grant.ID, &grant.ClientID, &grant.Name, &grant.RedirectURI, &grant.Scope, &grant.Permission, &grant.Status, &grant.CreatedAt, &grant.ExpiresAt, &grant.LastUsedAt)
	return grant, err
}

func (repo *mcpGrantRepository) Create(ctx context.Context, userID int64, grant service.MCPGrantDTO) (service.MCPGrantDTO, error) {
	row := repo.db.QueryRowContext(ctx, `INSERT INTO mcp_grants(id,user_id,client_id,name,redirect_uri,scope,permission,status,created_at,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id,client_id,name,redirect_uri,scope,permission,status,created_at,expires_at,last_used_at`, grant.ID, userID, grant.ClientID, grant.Name, grant.RedirectURI, grant.Scope, grant.Permission, grant.Status, grant.CreatedAt, grant.ExpiresAt)
	return scanMCPGrant(row)
}

func (repo *mcpGrantRepository) Revoke(ctx context.Context, userID int64, id string) (service.MCPGrantDTO, error) {
	row := repo.db.QueryRowContext(ctx, `UPDATE mcp_grants SET status='revoked',revoked_at=COALESCE(revoked_at,NOW()) WHERE id=$1 AND user_id=$2 RETURNING id,client_id,name,redirect_uri,scope,permission,status,created_at,expires_at,last_used_at`, id, userID)
	grant, err := scanMCPGrant(row)
	if errors.Is(err, sql.ErrNoRows) {
		return grant, service.ErrMCPGrantNotFound
	}
	return grant, err
}

func (repo *mcpGrantRepository) Active(ctx context.Context, userID int64, id string) (service.MCPGrantDTO, bool, error) {
	row := repo.db.QueryRowContext(ctx, `SELECT id,client_id,name,redirect_uri,scope,permission,status,created_at,expires_at,last_used_at FROM mcp_grants WHERE id=$1 AND user_id=$2 AND status='active' AND expires_at>NOW()`, id, userID)
	grant, err := scanMCPGrant(row)
	if errors.Is(err, sql.ErrNoRows) {
		return service.MCPGrantDTO{}, false, nil
	}
	return grant, err == nil, err
}

func (repo *mcpGrantRepository) RegisterClient(ctx context.Context, userID int64, clientID, name, redirectURI string) error {
	_, err := repo.db.ExecContext(ctx, `INSERT INTO mcp_clients(user_id,client_id,name,redirect_uri) VALUES($1,$2,$3,$4) ON CONFLICT(user_id,client_id) DO UPDATE SET name=EXCLUDED.name,redirect_uri=EXCLUDED.redirect_uri`, userID, clientID, name, redirectURI)
	return err
}

func (repo *mcpGrantRepository) ClientMatches(ctx context.Context, userID int64, clientID, redirectURI string) (bool, error) {
	var matches bool
	err := repo.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM mcp_clients WHERE user_id=$1 AND client_id=$2 AND redirect_uri=$3)`, userID, clientID, redirectURI).Scan(&matches)
	return matches, err
}
