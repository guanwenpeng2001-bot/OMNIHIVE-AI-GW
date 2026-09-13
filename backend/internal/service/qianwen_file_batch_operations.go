package service

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

type UpstreamResourceState struct {
	Deleted         bool   `json:"deleted,omitempty"`
	OutputDeleted   bool   `json:"output_deleted,omitempty"`
	CancelRequested bool   `json:"cancel_requested,omitempty"`
	ProviderStatus  string `json:"provider_status,omitempty"`
}

var ErrUpstreamResourceConflict = errors.New("resource is not ready or is in use")

// Optional extension keeps unrelated job adapters and repository fakes intact.
type UpstreamResourceRepository interface {
	ListResources(context.Context, int64, int64, int64, string, string, int) ([]*UpstreamJob, error)
	FileInUse(context.Context, string) (bool, error)
}

func (s *UpstreamJobService) ListResources(ctx context.Context, user, key, group int64, family, after string, limit int) ([]*UpstreamJob, error) {
	r, ok := s.repo.(UpstreamResourceRepository)
	if !ok {
		return nil, errors.New("resource store unavailable")
	}
	if limit < 1 || limit > 100 || (family != "files" && family != "batch" && family != "batch_input" && family != "batch_output") {
		return nil, ErrUpstreamResourceConflict
	}
	if after != "" {
		output := strings.HasPrefix(after, "output-")
		if output && family != "batch" {
			after = strings.TrimPrefix(after, "output-")
		}
		j, err := s.Get(ctx, after, user, key)
		expected := family
		if family == "batch_input" || family == "files" {
			expected = "files"
		}
		if family == "batch_output" || (family == "files" && output) {
			expected = "batch"
		}
		if err != nil || j.GroupID == nil || *j.GroupID != group || j.Family != expected || j.Platform != PlatformQianwen {
			return nil, ErrUpstreamJobNotFound
		}
	}
	return r.ListResources(ctx, user, key, group, family, after, limit)
}

func resourceOwned(j *UpstreamJob, user, key, group int64) bool {
	return j != nil && j.UserID == user && j.APIKeyID == key && j.GroupID != nil && *j.GroupID == group && j.Platform == PlatformQianwen
}

// Serialize child submission against input deletion using the durable parent lock.
func (s *UpstreamJobService) WithInputFile(ctx context.Context, id string, user, key, group int64, submit func() (*UpstreamJob, error)) (*UpstreamJob, error) {
	if _, err := s.Get(ctx, id, user, key); err != nil {
		return nil, err
	}
	var result *UpstreamJob
	_, err := s.repo.WithLocked(ctx, id, func(j *UpstreamJob, _ UpstreamJobTx) error {
		if !resourceOwned(j, user, key, group) || j.Family != "files" || j.ResourceState.Deleted {
			return ErrUpstreamJobNotFound
		}
		if j.Status != "succeeded" {
			return ErrUpstreamResourceConflict
		}
		var err error
		result, err = submit()
		return err
	})
	return result, err
}

func (s *UpstreamJobService) ManageResource(ctx context.Context, id string, user, key, group int64, action string, remote func(context.Context, *UpstreamJob, string) error) (*UpstreamJob, error) {
	if _, err := s.Get(ctx, id, user, key); err != nil {
		return nil, err
	}
	return s.repo.WithLocked(ctx, id, func(j *UpstreamJob, _ UpstreamJobTx) error {
		if !resourceOwned(j, user, key, group) {
			return ErrUpstreamJobNotFound
		}
		switch action {
		case "cancel":
			if j.Family != "batch" || j.ResourceState.Deleted {
				return ErrUpstreamJobNotFound
			}
			if j.Terminal() || j.ResourceState.CancelRequested {
				return nil
			}
			if j.UpstreamTaskID == "" {
				return ErrUpstreamResourceConflict
			}
		case "delete":
			if j.Family != "files" {
				return ErrUpstreamJobNotFound
			}
			if j.ResourceState.Deleted {
				return nil
			}
			if !j.Terminal() {
				return ErrUpstreamResourceConflict
			}
			r, ok := s.repo.(UpstreamResourceRepository)
			if !ok {
				return errors.New("resource store unavailable")
			}
			used, err := r.FileInUse(ctx, j.PublicRequestID)
			if err != nil {
				return err
			}
			if used {
				return ErrUpstreamResourceConflict
			}
		case "delete_output":
			if j.Family != "batch" {
				return ErrUpstreamJobNotFound
			}
			if j.ResourceState.OutputDeleted {
				return nil
			}
			if !j.Terminal() || j.ChargedUsageLogID == nil || j.ResultRef == "" {
				return ErrUpstreamResourceConflict
			}
		default:
			return ErrUpstreamResourceConflict
		}
		networkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := remote(networkCtx, j, action); err != nil {
			return err
		}
		switch action {
		case "cancel":
			j.ResourceState.CancelRequested = true
			j.ResourceState.ProviderStatus = "cancelling"
			j.NextPollAt = s.now()
		case "delete":
			j.ResourceState.Deleted = true
		case "delete_output":
			j.ResourceState.OutputDeleted = true
		}
		return nil
	})
}

func (s *OpenAIGatewayService) ManageQianwenResource(ctx context.Context, j *UpstreamJob, action string) error {
	a, err := s.QianwenResourceAccount(ctx, j, false)
	if err != nil {
		return err
	}
	method, path, id := http.MethodDelete, "/files/", j.UpstreamTaskID
	if action == "delete_output" {
		id = j.ResultRef
	}
	path += url.PathEscape(id)
	if action == "cancel" {
		method, path = http.MethodPost, "/batches/"+url.PathEscape(id)+"/cancel"
	}
	if id == "" {
		return nil
	} // failed upload has no remote file
	raw, status, err := s.qianwenResourceRequest(ctx, a, method, path, "application/json", nil)
	if err != nil {
		return err
	}
	if action != "cancel" && status == http.StatusNotFound {
		return nil
	} // retry after remote success/local rollback
	if status < 200 || status >= 300 || gjson.GetBytes(raw, "id").String() != id {
		return errors.New("resource operation rejected upstream")
	}
	if action == "cancel" {
		state := gjson.GetBytes(raw, "status").String()
		if state != "cancelling" && state != "cancelled" {
			return errors.New("invalid cancellation response")
		}
	} else if !gjson.GetBytes(raw, "deleted").Bool() {
		return errors.New("invalid deletion response")
	}
	return nil
}
