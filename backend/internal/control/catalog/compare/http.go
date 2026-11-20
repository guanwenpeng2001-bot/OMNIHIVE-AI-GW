package compare

import (
	"context"

	"github.com/gin-gonic/gin"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/catalog/quotes"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
)

func (s Service) Handle(ctx context.Context, p catalog.Perspective, c *gin.Context) (any, error) {
	var in Input
	if err := quotes.Decode(c, &in); err != nil {
		return nil, err
	}
	return s.Compare(ctx, p, in)
}
