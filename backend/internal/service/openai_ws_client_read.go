package service

import (
	"context"
	"errors"
	"sync"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
)

type openAIWSClientReadResult struct {
	messageType coderws.MessageType
	payload     []byte
	err         error
}

// One connection-owned reader survives account retries. It observes disconnects
// during silent generation and holds at most one queued follow-up turn.
type openAIWSIngressReader struct {
	ctx     context.Context
	frames  chan openAIWSClientReadResult
	stopped chan struct{}
	// Serialize consumers across attempt handoff. A frame dequeued concurrently
	// with cancellation is retained ahead of the socket queue for the next attempt.
	consumer sync.Mutex
	pending  *openAIWSClientReadResult
}

func openAIWSIngressClientReader(ginContext *gin.Context, connection *coderws.Conn) *openAIWSIngressReader {
	const readerKey = "openai_ws_ingress_reader"
	if existing, ok := ginContext.Get(readerKey); ok {
		reader, _ := existing.(*openAIWSIngressReader)
		return reader
	}
	readerContext, cancelReader := context.WithCancelCause(context.Background())
	reader := &openAIWSIngressReader{ctx: readerContext, frames: make(chan openAIWSClientReadResult, 1), stopped: make(chan struct{})}
	ginContext.Set(readerKey, reader)
	go func() {
		defer close(reader.stopped)
		defer cancelReader(context.Canceled)
		defer close(reader.frames)
		for {
			messageType, payload, readErr := connection.Read(context.Background())
			if readErr != nil {
				cancelReader(readErr)
				return
			}
			select {
			case reader.frames <- openAIWSClientReadResult{messageType: messageType, payload: payload}:
			default:
				// Never stop observing the socket behind an unbounded turn queue.
				cancelReader(errors.New("too many pending websocket turns"))
				_ = connection.Close(coderws.StatusPolicyViolation, "too many pending websocket turns")
				_ = connection.CloseNow()
				return
			}
		}
	}()
	return reader
}

// ReadOpenAIWSClientMessage keeps one reader alive while control events send
// their close frame, then closes the transport and joins that reader.
func ReadOpenAIWSClientMessage(
	controlCtx context.Context,
	conn *coderws.Conn,
	timeout time.Duration,
	timeoutStatus coderws.StatusCode,
	timeoutReason string,
) (coderws.MessageType, []byte, error) {
	return readOpenAIWSClientMessageWithTimeoutStart(
		controlCtx,
		conn,
		timeout,
		timeoutStatus,
		timeoutReason,
		nil,
		nil,
		nil,
		nil,
	)
}

// readOpenAIWSClientMessageWithTimeoutStart supports readers whose timeout
// starts after a state transition, such as a completed passthrough turn. When
// timeoutActive is nil, a positive timeout starts immediately.
func readOpenAIWSClientMessageWithTimeoutStart(
	controlCtx context.Context,
	conn *coderws.Conn,
	timeout time.Duration,
	timeoutStatus coderws.StatusCode,
	timeoutReason string,
	timeoutStart <-chan struct{},
	timeoutActive func() bool,
	reader *openAIWSIngressReader,
	attemptCtx context.Context,
) (coderws.MessageType, []byte, error) {
	if conn == nil {
		return 0, nil, errors.New("openai websocket client connection is nil")
	}
	if controlCtx == nil {
		controlCtx = context.Background()
	}

	var readDone <-chan openAIWSClientReadResult
	var readStopped <-chan struct{}
	var attemptDone <-chan struct{}
	if reader != nil {
		reader.consumer.Lock()
		defer reader.consumer.Unlock()
		readDone, readStopped = reader.frames, reader.stopped
		if attemptCtx != nil {
			attemptDone = attemptCtx.Done()
		}
	} else {
		incoming := make(chan openAIWSClientReadResult, 1)
		stopped := make(chan struct{})
		readDone, readStopped = incoming, stopped
		go func() {
			defer close(stopped)
			messageType, payload, err := conn.Read(context.Background())
			incoming <- openAIWSClientReadResult{messageType: messageType, payload: payload, err: err}
		}()
	}

	var timer *time.Timer
	var timeoutCh <-chan time.Time
	startTimeout := func() {
		if timeout <= 0 || (timeoutActive != nil && !timeoutActive()) {
			return
		}
		if timer == nil {
			timer = time.NewTimer(timeout)
		} else {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(timeout)
		}
		timeoutCh = timer.C
	}
	if timeoutActive == nil || timeoutActive() {
		startTimeout()
	}
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	closeAndJoin := func(status coderws.StatusCode, reason string, cause error) (coderws.MessageType, []byte, error) {
		_ = conn.Close(status, reason)
		_ = conn.CloseNow()
		<-readStopped
		return 0, nil, NewOpenAIWSClientCloseError(status, reason, cause)
	}

	for {
		// select does not prioritize cancellation over an already queued frame.
		// Check before dequeue and again before handing ownership to the caller.
		if reader != nil && attemptCtx != nil && attemptCtx.Err() != nil {
			return 0, nil, attemptCtx.Err()
		}
		if controlCtx.Err() != nil {
			return closeForCanceledControl(controlCtx, closeAndJoin)
		}
		if reader != nil && reader.pending != nil {
			result := reader.pending
			if attemptCtx != nil && attemptCtx.Err() != nil {
				return 0, nil, attemptCtx.Err()
			}
			reader.pending = nil
			return result.messageType, result.payload, result.err
		}
		select {
		case result, ok := <-readDone:
			if reader != nil && attemptCtx != nil && attemptCtx.Err() != nil {
				if ok {
					reader.pending = &result
				}
				return 0, nil, attemptCtx.Err()
			}
			if controlCtx.Err() != nil {
				return closeForCanceledControl(controlCtx, closeAndJoin)
			}
			if !ok {
				return 0, nil, context.Cause(reader.ctx)
			}
			return result.messageType, result.payload, result.err
		case <-attemptDone:
			// A failed account attempt relinquishes the queue consumer only;
			// the connection-owned reader remains available to the next attempt.
			return 0, nil, attemptCtx.Err()
		case <-timeoutStart:
			startTimeout()
		case <-timeoutCh:
			return closeAndJoin(timeoutStatus, timeoutReason, context.DeadlineExceeded)
		case <-controlCtx.Done():
			return closeForCanceledControl(controlCtx, closeAndJoin)
		}
	}
}

func closeForCanceledControl(controlCtx context.Context, closeAndJoin func(coderws.StatusCode, string, error) (coderws.MessageType, []byte, error)) (coderws.MessageType, []byte, error) {
	cause := context.Cause(controlCtx)
	if errors.Is(cause, ErrOpenAIWSIngressLeaseLost) {
		return closeAndJoin(coderws.StatusTryAgainLater, "websocket ingress capacity lease lost; please reconnect", cause)
	}
	return closeAndJoin(coderws.StatusGoingAway, "websocket request canceled", cause)
}
