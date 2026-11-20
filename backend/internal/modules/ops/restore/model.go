package restore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
)

const (
	Isolated      = "isolated"
	Production    = "production"
	PreviewAction = "ops.restore.preview"
	ExecuteAction = "ops.restore.execute"
)

var (
	ErrLeaseLost   = errors.New("restore lease lost")
	Unavailable    = &fault.Fault{Status: 503, Reason: "RESTORE_UNAVAILABLE", Message: "Restore infrastructure is unavailable."}
	busy           = &fault.Fault{Status: 409, Reason: "RESTORE_TARGET_BUSY", Message: "The target has an active restore or requires review."}
	mismatch       = &fault.Fault{Status: 409, Reason: "RESTORE_SOURCE_CHANGED", Message: "The archive does not match the verified source."}
	prerequisite   = &fault.Fault{Status: 409, Reason: "RESTORE_PRECONDITION", Message: "A fresh target preview and matching verified rehearsal are required."}
	invalidArchive = &fault.Fault{Status: 422, Reason: "RESTORE_ARCHIVE_INVALID", Message: "The archive could not be verified as a complete OmniHive database dump."}
)

type Actor struct {
	Workspace, ID int64
	Session       string
}
type StepUp interface {
	Require(context.Context, Actor) error
}
type PreviewInput struct {
	BackupID       string `json:"backup_id"`
	Target         string `json:"target"`
	ExpectedDigest string `json:"expected_digest"`
}
type ExecuteInput struct {
	PreviewID   string `json:"preview_id"`
	Target      string `json:"target"`
	RehearsalID string `json:"rehearsal_id"`
}
type Preview struct {
	ID        string            `json:"id"`
	Verified  bool              `json:"verified"`
	Impact    map[string]string `json:"impact"`
	ExpiresAt time.Time         `json:"expires_at"`
}
type Report struct {
	Digest string            `json:"source_digest,omitempty"`
	Counts map[string]string `json:"counts,omitempty"`
	Reason string            `json:"reason,omitempty"`
}
type Operation struct {
	ID         string  `json:"id"`
	Generation int64   `json:"generation,string"`
	State      string  `json:"state"`
	Stage      string  `json:"stage"`
	Target     string  `json:"target"`
	Report     *Report `json:"report"`
}
type Snapshot struct {
	Key    string `json:"key"`
	Digest string `json:"digest"`
}

// Archive records contain the legacy single-object or multipart locator, never storage credentials.
type Archives interface {
	Record(context.Context, string) (json.RawMessage, error)
	Open(context.Context, json.RawMessage) (io.ReadCloser, error)
	Snapshot(context.Context, string, io.ReadCloser) (Snapshot, error)
}
type Database interface {
	Dump(context.Context) (io.ReadCloser, error)
	RestoreChecked(context.Context, string, string, func() error, func() error, func() error) error
}
type Target struct {
	DB       *sql.DB
	Database Database
}
type Service struct {
	Journal  *sql.DB
	Access   identity.AccessEvaluator
	StepUp   StepUp
	Archives Archives
	Targets  map[string]Target
}

type lease struct {
	Operation
	Actor         Actor
	Owner, Digest string
	Record        json.RawMessage
	Evidence      map[string]string
}
