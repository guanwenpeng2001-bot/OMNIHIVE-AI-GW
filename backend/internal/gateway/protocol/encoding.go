package protocol

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/gatewayerror"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/googleapi"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/util/logredact"
)

func WriteSSE(w io.Writer, event string, data any) {
	if event != "" {
		_, _ = fmt.Fprintf(w, "event: %s\n", event)
	}
	b, _ := json.Marshal(data)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", string(b))
}

func WriteMessagesError(c *gin.Context, status int, errType, message string) {
	c.JSON(status, gin.H{
		"type":  "error",
		"error": gin.H{"type": errType, "message": message},
	})
}

func WriteGeminiError(c *gin.Context, status int, message string) string {
	message = logredact.RedactDiagnostic(message)
	c.JSON(status, gin.H{
		"error": gin.H{
			"code": status, "message": message,
			"status": googleapi.HTTPStatusToGoogleStatus(status),
		},
	})
	return message
}

func WriteStreamError(c *gin.Context, status int, errType, code, message string) {
	if code == "" {
		code = errType
	}
	gatewayerror.WriteStream(c, status, code, message)
}
