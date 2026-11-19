// Package tasks owns the image-task journal and project-scoped task projections.
package tasks

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/tasks/media"
)

var (
	LostLease         = errors.New("image task lease lost")
	Unknown           = &fault.Fault{Status: 409, Reason: "SUBMISSION_UNKNOWN", Message: "The provider may have accepted the task. Query its status before retrying."}
	UnsupportedCancel = &fault.Fault{Status: 409, Reason: "CANCEL_UNSUPPORTED", Message: "Cancellation is unavailable at this stage."}
	NotRetryable      = &fault.Fault{Status: 409, Reason: "RETRY_UNAVAILABLE", Message: "A safely retryable item with retained input is required."}
	Gone              = &fault.Fault{Status: 410, Reason: "ARTIFACT_GONE", Message: "This artifact has expired or was revoked."}
	Unavailable       = &fault.Fault{Status: 503, Reason: "DEPENDENCY_UNAVAILABLE", Message: "Task service temporarily unavailable."}
	Budget            = &fault.Fault{Status: 402, Reason: "BUDGET_EXHAUSTED", Message: "Insufficient funding for a new task."}
)

type Scope = identity.Access

type Counts struct {
	Total     int64 `json:"total,string"`
	Succeeded int64 `json:"succeeded,string"`
	Failed    int64 `json:"failed,string"`
	Pending   int64 `json:"pending,string"`
	Canceled  int64 `json:"canceled,string"`
}
type Artifact struct {
	ID          string     `json:"id"`
	ContentType string     `json:"content_type"`
	Size        *int64     `json:"size_bytes,string"`
	Expires     *time.Time `json:"expires_at"`
	State       string     `json:"state"`
}
type Detail struct {
	ID             string            `json:"id"`
	Project        int64             `json:"project_id,string"`
	Kind           string            `json:"kind"`
	Source         string            `json:"source"`
	Model          string            `json:"model_id"`
	Request        *string           `json:"request_id"`
	RequestURL     *string           `json:"request_url"`
	SettlementID   *string           `json:"settlement_id"`
	SettlementURL  *string           `json:"settlement_url"`
	Parent         *string           `json:"parent_task_id"`
	State          string            `json:"state"`
	Stage          string            `json:"stage"`
	Provider       string            `json:"provider_state"`
	ArtifactState  string            `json:"artifact_state"`
	Cancel         string            `json:"cancel_state"`
	Progress       *int              `json:"progress"`
	ProgressSource string            `json:"progress_source"`
	Counts         Counts            `json:"items"`
	Artifacts      []Artifact        `json:"artifacts"`
	Settlement     string            `json:"settlement_state"`
	Estimate       *money.Money      `json:"estimate"`
	Charge         *money.Money      `json:"actual_charge"`
	Created        time.Time         `json:"created_at"`
	Updated        time.Time         `json:"updated_at"`
	Actions        []string          `json:"allowed_actions"`
	Reasons        map[string]string `json:"action_reasons"`
	Version        int64             `json:"version,string"`
	owner, key     int64
	replayable     bool
	sourceID       string
	providerJob    *string
}
type Item struct {
	ID         string   `json:"id"`
	State      string   `json:"state"`
	Acceptance string   `json:"acceptance"`
	Reason     *string  `json:"reason"`
	Retryable  bool     `json:"retryable"`
	Artifacts  []string `json:"artifact_ids"`
}
type Input struct {
	Prompt   string `json:"prompt"`
	Size     string `json:"size,omitempty"`
	Count    int    `json:"count,omitempty"`
	Format   string `json:"format,omitempty"`
	Duration int    `json:"duration_seconds,omitempty"`
}
type Create struct {
	Kind         string `json:"kind"`
	Model        string `json:"model_id"`
	Key          int64  `json:"api_key_id,string"`
	Input        Input  `json:"input"`
	Acknowledged bool   `json:"funding_acknowledged"`
}
type Retry struct {
	Items        []string `json:"item_ids"`
	Acknowledged bool     `json:"funding_acknowledged"`
}
type RetryResult struct {
	ID      string `json:"task_id"`
	Parent  string `json:"parent_task_id"`
	Request string `json:"new_request_id"`
	State   string `json:"state"`
}
type CancelResult struct {
	ID      string `json:"task_id"`
	Cancel  string `json:"cancel_state"`
	State   string `json:"state"`
	Version int64  `json:"version,string"`
}
type ArtifactContent struct {
	Artifact
	Source, Locator, SourceID, ItemID string
	Owner, Key                        int64
}
type ObjectReader interface {
	Open(context.Context, ArtifactContent) (io.ReadCloser, error)
}
type Cipher interface {
	Encrypt(string) (string, error)
	Decrypt(string) (string, error)
}
type Runner interface {
	// Prepare revalidates key, model/adapter, funding and scope, then admits
	// the supplied request identity. Run invokes that plan at most once.
	Prepare(context.Context, Scope, Work) (Prepared, error)
}
type Work struct {
	ID, Request, Model, IP, Action, Kind string
	Lease                                Lease
	Key                                  int64
	Input                                Input
}
type Prepared struct {
	Media   *media.Proof
	Version string
	Run     func(context.Context) Outcome
	Close   func()
}
type Outcome struct {
	DurationMillis    int64
	State, Acceptance string
	Progress          *int
	Artifacts         []Output
}
type Output struct {
	ID, Locator, ContentType string
	Size                     int64
}
type BatchCanceler interface {
	CancelBatch(context.Context, int64, int64, string) error
}
type VideoCanceler interface {
	CancelVideo(context.Context, int64, int64, string) error
}
type Store struct {
	Video   VideoCanceler
	DB      *sql.DB
	Runner  Runner
	Cipher  Cipher
	Objects ObjectReader
	Batch   BatchCanceler
	slots   chan struct{}
}

func New(db *sql.DB, r Runner, c Cipher, o ObjectReader, b BatchCanceler) *Store {
	return &Store{DB: db, Runner: r, Cipher: c, Objects: o, Batch: b, slots: make(chan struct{}, 8)}
}
func Base(s Scope) string {
	return fmt.Sprintf("/api/v2/workspaces/%d/projects/%d", s.WorkspaceID, s.ProjectID)
}
func TaskURL(s Scope, id string) string { return Base(s) + "/tasks/" + url.PathEscape(id) }
func str(i int64) string                { return strconv.FormatInt(i, 10) }
func found(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fault.NotFound
	}
	return err
}
