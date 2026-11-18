package middleware

import (
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/ctxkey"
	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/response"
)

// Recovery converts panics into the project's standard JSON error envelope.
//
// It preserves Gin's broken-pipe handling by not attempting to write a response
// when the client connection is already gone.
func Recovery() gin.HandlerFunc {
	panicLogger := log.New(gin.DefaultErrorWriter, "[Recovery] ", log.LstdFlags)
	// Gin's default debug/broken-pipe logger dumps the request URL and headers
	// while masking only Authorization. Never send cookies, API keys, or query
	// credentials to that writer. Panic values are arbitrary, potentially secret
	// data too: log only their type, the correlation ID, and the stack.
	return gin.CustomRecoveryWithWriter(nil, func(c *gin.Context, recovered any) {
		if recovered == http.ErrAbortHandler {
			panic(recovered)
		}
		requestID, _ := c.Request.Context().Value(ctxkey.RequestID).(string)
		panicLogger.Printf("panic recovered: type=%T request_id=%q\n%s",
			recovered, requestID, debug.Stack())
		recoveredErr, _ := recovered.(error)

		if isBrokenPipe(recoveredErr) {
			if recoveredErr != nil {
				_ = c.Error(recoveredErr)
			}
			c.Abort()
			return
		}

		if c.Writer.Written() {
			c.Abort()
			return
		}

		response.ErrorWithDetails(
			c,
			http.StatusInternalServerError,
			infraerrors.UnknownMessage,
			infraerrors.UnknownReason,
			nil,
		)
		c.Abort()
	})
}

func isBrokenPipe(err error) bool {
	if err == nil {
		return false
	}

	var opErr *net.OpError
	if !errors.As(err, &opErr) {
		return false
	}

	var syscallErr *os.SyscallError
	if !errors.As(opErr.Err, &syscallErr) {
		return false
	}

	msg := strings.ToLower(syscallErr.Error())
	return strings.Contains(msg, "broken pipe") || strings.Contains(msg, "connection reset by peer")
}
