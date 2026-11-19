package handler

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/tasks"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/tasks/media"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/providers/mediajobs"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

func (g *ImageTaskGateway) mediaProfile(ctx context.Context, a tasks.Scope, w tasks.Work) (media.Profile, error) {
	snap, err := g.Catalog.Snapshot(ctx, catalog.Perspective{Actor: a.ActorID, Workspace: a.WorkspaceID, Project: a.ProjectID, AccessVersion: a.Version, EndpointMetadata: true}, time.Now().UTC())
	if err != nil {
		return media.Profile{}, err
	}
	for _, d := range snap.Details {
		if d.Model.ID == w.Model && d.Model.Provider == service.PlatformQianwen && d.Model.AccessState == "allowed" && d.Model.RouteState == "eligible" {
			if p, ok := media.Lookup(w.Kind, d.Model.Model); ok {
				return p, nil
			}
		}
	}
	return media.Profile{}, media.Unsupported
}
func (g *ImageTaskGateway) mediaProof(ctx context.Context, a tasks.Scope, key *service.APIKey, p media.Profile, account int64) (media.Proof, error) {
	rate, err := g.Images.gatewayService.QianwenTaskRate(ctx, key, p)
	if err != nil {
		return media.Proof{}, media.Unsupported
	}
	return mediajobs.Verified(ctx, g.DB, a.WorkspaceID, a.ProjectID, key.Group.ID, account, p, rate)
}

func taskPayload(w tasks.Work, model string) []byte {
	var payload map[string]any
	switch w.Kind {
	case "audio":
		payload = map[string]any{"model": model, "input": w.Input.Prompt, "response_format": "wav", "voice": "Cherry"}
	case "video":
		payload = map[string]any{"model": model, "prompt": w.Input.Prompt, "size": w.Input.Size, "seconds": w.Input.Duration}
	default:
		payload = map[string]any{"model": model, "prompt": w.Input.Prompt, "n": w.Input.Count}
		if w.Input.Size != "" {
			payload["size"] = w.Input.Size
		}
	}
	data, _ := json.Marshal(payload)
	return data
}

func (g *ImageTaskGateway) runKind(c *gin.Context, kind string) {
	switch kind {
	case "audio":
		g.Images.QianwenAudioSpeech(c)
	case "video":
		g.Images.QianwenVideoGeneration(c)
	default:
		g.Images.Images(c)
	}
}

// The native adapter returns the provider WAV. Confirm its container and length
// before committing a private artifact, independent of the existing receipt.
func saveTaskAudio(ctx context.Context, objects service.BackupObjectStore, id string, b []byte) tasks.Outcome {
	fail := tasks.Outcome{State: "failed", Acceptance: "accepted"}
	ms, ok := media.WAVDuration(b)
	if !ok {
		return fail
	}
	path := "task-artifacts/" + id + "/audio.wav"
	n, err := objects.Upload(ctx, path, bytes.NewReader(b), "audio/wav")
	if err != nil || n != int64(len(b)) {
		return fail
	}
	return tasks.Outcome{State: "succeeded", Acceptance: "accepted", DurationMillis: ms, Artifacts: []tasks.Output{{ID: "audio_1", Locator: path, ContentType: "audio/wav", Size: n}}}
}

type taskVideoResults struct{ native *OpenAIGatewayHandler }

func (v taskVideoResults) job(ctx context.Context, owner, key int64, id string) (*service.UpstreamJob, error) {
	if v.native == nil || v.native.upstreamJobs == nil || v.native.gatewayService == nil {
		return nil, tasks.Unavailable
	}
	j, err := v.native.upstreamJobs.Get(ctx, id, owner, key)
	if err != nil {
		return nil, err
	}
	if j.Platform != service.PlatformQianwen || j.Family != "video" || j.UserID != owner || j.APIKeyID != key {
		return nil, tasks.Unavailable
	}
	return j, nil
}
func (v taskVideoResults) CancelVideo(ctx context.Context, owner, key int64, id string) error {
	j, err := v.job(ctx, owner, key, id)
	if err != nil {
		return err
	}
	_, err = v.native.gatewayService.CancelQianwenVideo(ctx, j)
	return err
}
func (v taskVideoResults) Open(ctx context.Context, c tasks.ArtifactContent) (io.ReadCloser, error) {
	j, err := v.job(ctx, c.Owner, c.Key, c.Locator)
	if err != nil {
		return nil, err
	}
	if j.Status != "succeeded" || !j.ExpiresAt.After(time.Now()) {
		return nil, tasks.Gone
	}
	account, err := v.native.gatewayService.QianwenResourceAccount(ctx, j, false)
	if err != nil {
		return nil, tasks.Unavailable
	}
	resp, err := qianwenVideoContentOpener(ctx, account, j.ResultRef)
	if errors.Is(err, service.ErrQianwenVideoResultGone) {
		return nil, tasks.Gone
	}
	if err != nil {
		return nil, tasks.Unavailable
	}
	if resp.StatusCode != http.StatusOK || resp.ContentLength <= 0 || resp.ContentLength > media.MaxVideoBytes {
		resp.Body.Close()
		return nil, tasks.Unavailable
	}
	stream := bufio.NewReader(resp.Body)
	header, err := stream.Peek(int(min(resp.ContentLength, 512)))
	if err != nil || http.DetectContentType(header) != "video/mp4" {
		resp.Body.Close()
		return nil, tasks.Unavailable
	}
	return struct {
		io.Reader
		io.Closer
	}{io.LimitReader(stream, resp.ContentLength), resp.Body}, nil
}
