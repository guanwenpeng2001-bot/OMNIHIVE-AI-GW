package handler

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/tasks"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/httpclient"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

// ImageTaskArtifacts uses the original owner for batch reads after the
// control plane has authorized the canonical workspace, project and artifact.
type ImageTaskArtifacts struct {
	Objects  service.BackupObjectStore
	Batches  *service.BatchImageDownloadService
	Commands *service.BatchImagePublicService
	delivery *http.Client
}

func NewImageTaskArtifacts(o service.BackupObjectStore, b *service.BatchImageDownloadService, c *service.BatchImagePublicService) *ImageTaskArtifacts {
	client := &http.Client{Transport: httpclient.NewPublicTransport(&http.Transport{}), Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, prior []*http.Request) error {
			if len(prior) >= 3 || req.URL.Scheme != "https" || req.URL.User != nil {
				return errors.New("invalid image delivery redirect")
			}
			return nil
		}}
	return &ImageTaskArtifacts{o, b, c, client}
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
