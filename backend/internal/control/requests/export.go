package requests

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	domain "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/requests"
)

var exportExpired = &fault.Fault{Status: http.StatusGone, Reason: "ARTIFACT_EXPIRED", Message: "The export has expired."}

func (h *Handler) startExport(r request) (any, error) {
	var input struct {
		Format   string   `json:"format"`
		Sections []string `json:"sections"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(r.Writer, r.Request.Body, 8<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || decoder.Decode(new(any)) != io.EOF {
		return nil, &fault.Fault{Status: http.StatusBadRequest, Reason: "INVALID_JSON", Message: "Invalid request body."}
	}
	if input.Format != "json" {
		return nil, fault.Invalid("/format")
	}
	allowed := []string{"attempts", "billing", "route", "timeline", "usage"}
	if len(input.Sections) == 0 || len(input.Sections) > len(allowed) {
		return nil, fault.Invalid("/sections")
	}
	slices.Sort(input.Sections)
	for i, section := range input.Sections {
		if !slices.Contains(allowed, section) || (i > 0 && section == input.Sections[i-1]) {
			return nil, fault.Invalid("/sections")
		}
	}
	keys := r.Request.Header.Values("Idempotency-Key")
	if len(keys) != 1 || strings.TrimSpace(keys[0]) == "" || len(keys[0]) > 256 {
		return nil, fault.Invalid("/Idempotency-Key")
	}
	ex, err := h.store.StartExport(r.Request.Context(), r.view.Scope, r.actor, r.id, keys[0], input.Sections)
	if err != nil {
		return nil, err
	}
	return operation(ex, r.actor), nil
}

func operation(ex domain.Export, actor int64) identity.Operation {
	result := struct {
		DownloadURL *string   `json:"download_url"`
		ExpiresAt   time.Time `json:"expires_at"`
	}{ExpiresAt: ex.ExpiresAt}
	if time.Now().Before(ex.ExpiresAt) {
		url := ex.RequestURL(ex.RequestID) + "/exports/" + ex.ID + "/download"
		result.DownloadURL = &url
	}
	return identity.Operation{
		ID: ex.ID, State: "succeeded", StatusURL: fmt.Sprintf("/api/v2/workspaces/%d/operations/%s", ex.WorkspaceID, ex.ID),
		ResourceID: &ex.RequestID, ProjectID: ex.ProjectID, OwnerID: actor, Result: result,
	}
}

// ReadOperation is registered with the BE-01 operations dispatcher.
func (h *Handler) ReadOperation(ctx context.Context, workspace, actor int64, id string) (identity.Operation, error) {
	if !validUUID(id) {
		return identity.Operation{}, fault.NotFound
	}
	ex, err := h.store.ExportOperation(ctx, workspace, actor, id)
	if err != nil {
		return identity.Operation{}, err
	}
	if _, err := h.authorize(ctx, actor, ex.Scope, true); err != nil {
		return identity.Operation{}, err
	}
	return operation(ex, actor), nil
}

var _ identity.OperationReader = (*Handler)(nil)

func (h *Handler) download(r request) (any, error) {
	id := r.Param("export_id")
	if !validUUID(id) {
		return nil, fault.NotFound
	}
	ctx := r.Request.Context()
	ex, err := h.store.ScopedExport(ctx, r.view.Scope, r.actor, r.id, id)
	if err != nil {
		return nil, err
	}
	if !time.Now().Before(ex.ExpiresAt) {
		return nil, exportExpired
	}
	detail, err := h.store.Detail(ctx, r.view, r.id)
	if err != nil {
		return nil, err
	}
	a, err := h.authorize(ctx, r.actor, ex.Scope, true)
	if err != nil {
		return nil, err
	}
	if perspective(a) != r.view {
		return nil, &fault.Fault{Status: http.StatusConflict, Reason: "ACCESS_CHANGED", Message: "Request access has changed. Retry the download."}
	}
	if !time.Now().Before(ex.ExpiresAt) {
		return nil, exportExpired
	}
	r.Header("Content-Type", "application/json")
	r.Header("Content-Disposition", `attachment; filename="request-metadata.json"`)
	r.Header("X-Content-Type-Options", "nosniff")
	return nil, streamExport(r.Writer, detail, ex.Sections)
}

func streamExport(w io.Writer, detail domain.Detail, sections []string) error {
	if _, err := io.WriteString(w, "{"); err != nil {
		return err
	}
	write := func(name string, value any) error {
		// Names are internal constants or sections validated at registration.
		if _, err := fmt.Fprintf(w, "%q:", name); err != nil {
			return err
		}
		return json.NewEncoder(w).Encode(value)
	}
	meta := struct {
		ID               string              `json:"id"`
		WorkspaceID      int64               `json:"workspace_id,string"`
		ProjectID        int64               `json:"project_id,string"`
		Redactions       []string            `json:"redactions"`
		DataCompleteness domain.Completeness `json:"data_completeness"`
	}{detail.ID, detail.WorkspaceID, detail.ProjectID, detail.Redactions, detail.DataCompleteness}
	if err := write("request", meta); err != nil {
		return err
	}
	for _, section := range sections {
		if _, err := io.WriteString(w, ","); err != nil {
			return err
		}
		var err error
		switch section {
		case "timeline":
			err = write(section, detail.Timeline)
		case "route":
			err = write(section, detail.Route)
		case "attempts":
			err = write(section, detail.Attempts)
			if err == nil {
				_, err = io.WriteString(w, ",")
			}
			if err == nil {
				err = write("abandoned_attempts", detail.AbandonedAttempts)
			}
		case "usage":
			err = write(section, detail.Usage)
		case "billing":
			err = write(section, detail.Billing)
		}
		if err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, "}\n")
	return err
}
