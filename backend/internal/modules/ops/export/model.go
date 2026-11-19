// Package export manages workspace-approved operational timing exports.
package export

import (
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/util/urlvalidator"
)

const (
	ConfigureGrant = "ops.export.configure"

	maxActiveExports = 8
	maxPending       = 256
	maxCompleted     = 512
	maxAttempts      = 4
	maxEndpointBytes = 2048
	sampleScale      = 1_000_000
	timings          = "timings"

	stateValidating = "validating"
	stateActive     = "active"
	stateFailed     = "failed"
	stateDisabled   = "disabled"

	reasonUnavailable        = "DESTINATION_UNAVAILABLE"
	reasonRejected           = "DESTINATION_REJECTED"
	reasonExhausted          = "RETRY_EXHAUSTED"
	reasonDestinationRevoked = "DESTINATION_REVOKED"
	reasonApprovalRevoked    = "APPROVAL_REVOKED"
)

var errCapacity = &fault.Fault{Status: http.StatusTooManyRequests, Reason: "EXPORT_CAPACITY", Message: "Workspace export capacity reached."}

type Caller struct {
	ActorID, WorkspaceID int64
}

type Destination struct {
	ID      uuid.UUID `json:"id"`
	State   string    `json:"state"`
	Version int64     `json:"version,string"`
}

type Config struct {
	ID            uuid.UUID `json:"id"`
	State         string    `json:"state"`
	Version       int64     `json:"version,string"`
	DestinationID uuid.UUID `json:"destination_id"`
}

type Detail struct {
	Config
	DataClasses     []string   `json:"data_classes"`
	SampleRate      string     `json:"sample_rate"`
	RetentionDays   int        `json:"retention_days"`
	LastSuccessAt   *time.Time `json:"last_success_at"`
	LastErrorReason *string    `json:"last_error_reason"`
	QueueDepth      int        `json:"queue_depth,string"`
}

type CreateInput struct {
	DestinationID uuid.UUID `json:"destination_id"`
	DataClasses   []string  `json:"data_classes"`
	SampleRate    string    `json:"sample_rate"`
	RetentionDays int       `json:"retention_days"`
}

type PatchInput struct {
	DestinationID *uuid.UUID `json:"destination_id"`
	DataClasses   *[]string  `json:"data_classes"`
	SampleRate    *string    `json:"sample_rate"`
	RetentionDays *int       `json:"retention_days"`
	Enabled       *bool      `json:"enabled"`
}

type policy struct {
	DestinationID uuid.UUID
	SamplePPM     int
	RetentionDays int
}

func parsePolicy(input CreateInput) (policy, error) {
	if input.DestinationID == uuid.Nil {
		return policy{}, fault.Invalid("/destination_id")
	}
	if len(input.DataClasses) != 1 || input.DataClasses[0] != timings {
		return policy{}, fault.Invalid("/data_classes")
	}
	rate, err := parseRate(input.SampleRate)
	if err != nil {
		return policy{}, err
	}
	if input.RetentionDays < 1 || input.RetentionDays > 30 {
		return policy{}, fault.Invalid("/retention_days")
	}
	return policy{input.DestinationID, rate, input.RetentionDays}, nil
}

var decimalRate = regexp.MustCompile(`^(0(\.[0-9]{1,6})?|1(\.0{1,6})?)$`)

func parseRate(raw string) (int, error) {
	if !decimalRate.MatchString(raw) {
		return 0, fault.Invalid("/sample_rate")
	}
	if raw[0] == '1' {
		return sampleScale, nil
	}
	_, fraction, _ := strings.Cut(raw, ".")
	ppm, _ := strconv.Atoi(fraction + strings.Repeat("0", 6-len(fraction)))
	return ppm, nil
}

func formatRate(ppm int) string {
	if ppm == sampleScale {
		return "1"
	}
	if ppm == 0 {
		return "0"
	}
	digits := strconv.Itoa(sampleScale + ppm)[1:]
	return "0." + strings.TrimRight(digits, "0")
}

func parseEndpoint(raw string) (string, error) {
	if len(raw) > maxEndpointBytes || strings.TrimSpace(raw) != raw || strings.Contains(raw, "#") {
		return "", fault.Invalid("/endpoint")
	}
	if _, err := urlvalidator.ValidateHTTPSURL(raw, urlvalidator.ValidationOptions{}); err != nil {
		return "", fault.Invalid("/endpoint")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Opaque != "" || u.ForceQuery || u.RawQuery != "" || u.Fragment != "" {
		return "", fault.Invalid("/endpoint")
	}
	// Preserve the collector's path, including escapes and a trailing slash.
	canonical := u.String()
	if len(canonical) > maxEndpointBytes {
		return "", fault.Invalid("/endpoint")
	}
	return canonical, nil
}
