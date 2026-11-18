package service

import (
	"context"
	"net/http"
	"strings"
	"time"

	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
)

var (
	ErrBatchImageQueueEmpty          = infraerrors.New(http.StatusNotFound, "BATCH_IMAGE_QUEUE_EMPTY", "batch image queue is empty")
	ErrBatchImageAlreadyQueued       = infraerrors.New(http.StatusConflict, "BATCH_IMAGE_ALREADY_QUEUED", "batch image job is already queued")
	ErrBatchImageLockNotAcquired     = infraerrors.New(http.StatusConflict, "BATCH_IMAGE_LOCK_NOT_ACQUIRED", "batch image job lock was not acquired")
	ErrInvalidBatchImageQueuePayload = infraerrors.New(http.StatusBadRequest, "BATCH_IMAGE_QUEUE_INVALID_PAYLOAD", "invalid batch image queue payload")
)

type ReservedBatchImageJob struct {
	BatchID string
}

type BatchImageJobLock interface {
	Release(ctx context.Context) error
}

type BatchImageQueue interface {
	Enqueue(ctx context.Context, batchID string) error
	Reserve(ctx context.Context, blockTimeout time.Duration) (ReservedBatchImageJob, error)
	RequeueAfter(ctx context.Context, batchID string, delay time.Duration) error
	Ack(ctx context.Context, batchID string) error
	Heartbeat(ctx context.Context, batchID string) error
	MoveDueDelayedToReady(ctx context.Context, limit int) (int, error)
	RecoverStaleActive(ctx context.Context, staleAfter time.Duration, limit int) (int, error)
	TryAcquireJobLock(ctx context.Context, batchID string, ttl time.Duration) (BatchImageJobLock, bool, error)
}

func IsValidBatchImageID(batchID string) bool {
	return strings.HasPrefix(batchID, "imgbatch_") && len(batchID) > len("imgbatch_")
}
