package quotes

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
)

func (s Service) Handle(ctx context.Context, p catalog.Perspective, c *gin.Context) (any, error) {
	var in Input
	if err := Decode(c, &in); err != nil {
		return nil, err
	}
	return s.Quote(ctx, p, in)
}

func Decode(c *gin.Context, value any) error {
	if c.Request.URL.RawQuery != "" {
		return fault.Invalid("/query")
	}
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil || decoder.Decode(new(any)) != io.EOF {
		return &fault.Fault{Status: 400, Reason: "INVALID_JSON", Message: "Invalid request body."}
	}
	return nil
}
