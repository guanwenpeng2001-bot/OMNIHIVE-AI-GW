package handler

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/tasks"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

// ImageTaskArtifacts uses the original owner for batch reads after the
// control plane has authorized the canonical workspace, project and artifact.
type ImageTaskArtifacts struct {
	Objects  service.BackupObjectStore
	Batches  *service.BatchImageDownloadService
	Commands *service.BatchImagePublicService
	video    taskVideoResults
	delivery *service.ControlledEgress
}

func NewImageTaskArtifacts(o service.BackupObjectStore, b *service.BatchImageDownloadService, c *service.BatchImagePublicService) *ImageTaskArtifacts {
	return &ImageTaskArtifacts{Objects: o, Batches: b, Commands: c, delivery: service.NewArtifactEgress()}
}
func (r *ImageTaskArtifacts) CancelBatch(ctx context.Context, user, key int64, id string) error {
	if r.Commands == nil {
		return tasks.Unavailable
	}
	_, e := r.Commands.Cancel(ctx, service.BatchImageOwner{UserID: user, APIKeyID: key}, id)
	return e
}
func (r *ImageTaskArtifacts) Open(ctx context.Context, v tasks.ArtifactContent) (io.ReadCloser, error) {
	switch v.Source {
	case "media_video":
		return r.video.Open(ctx, v)
	case "gateway":
		if r.Objects == nil || !strings.HasPrefix(v.Locator, "task-artifacts/"+v.SourceID+"/") {
			return nil, tasks.Unavailable
		}
		return r.Objects.Download(ctx, v.Locator)
	case "batch_image":
		if r.Batches == nil {
			return nil, tasks.Unavailable
		}
		index, e := strconv.Atoi(v.Locator)
		if e != nil {
			return nil, e
		}
		stream, e := r.Batches.OpenItemContent(ctx, service.BatchImageOwner{UserID: v.Owner, APIKeyID: v.Key}, v.SourceID, v.ItemID, index)
		if e != nil {
			return nil, e
		}
		return stream.Reader, nil
	case "upstream_job", "redis_image":
		u, e := url.Parse(v.Locator)
		if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
			return nil, tasks.Gone
		}
		req, e := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
		if e != nil {
			return nil, e
		}
		resp, e := r.delivery.Do(req)
		if e != nil {
			return nil, tasks.Unavailable
		}
		defer resp.Body.Close()
		if resp.StatusCode == 404 || resp.StatusCode == 410 {
			return nil, tasks.Gone
		}
		if resp.StatusCode != 200 {
			return nil, tasks.Unavailable
		}
		const limit = 32 << 20
		data, e := io.ReadAll(io.LimitReader(resp.Body, limit+1))
		if e != nil || len(data) > limit || !strings.HasPrefix(http.DetectContentType(data), "image/") {
			return nil, tasks.Unavailable
		}
		return io.NopCloser(bytes.NewReader(data)), nil
	default:
		return nil, tasks.Unavailable
	}
}

type taskArtifactObjects struct {
	store   service.BackupObjectStore
	enabled bool
}

func (o *taskArtifactObjects) writable() bool {
	return o.enabled && o.store != nil
}

func (o *taskArtifactObjects) Upload(ctx context.Context, key string, body io.Reader, contentType string) (int64, error) {
	if !o.writable() {
		return 0, tasks.Unavailable
	}
	return o.store.Upload(ctx, key, body, contentType)
}

func (o *taskArtifactObjects) UploadFile(ctx context.Context, key, filename, contentType string) (int64, error) {
	if !o.writable() {
		return 0, tasks.Unavailable
	}
	return o.store.UploadFile(ctx, key, filename, contentType)
}

func (o *taskArtifactObjects) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	if o.store == nil {
		return nil, tasks.Unavailable
	}
	return o.store.Download(ctx, key)
}

func (o *taskArtifactObjects) Delete(ctx context.Context, key string) error {
	if o.store == nil {
		return tasks.Unavailable
	}
	return o.store.Delete(ctx, key)
}

func (o *taskArtifactObjects) PresignURL(context.Context, string, time.Duration) (string, error) {
	return "", tasks.Unavailable
}

func (o *taskArtifactObjects) HeadBucket(ctx context.Context) error {
	if o.store == nil {
		return tasks.Unavailable
	}
	return o.store.HeadBucket(ctx)
}
