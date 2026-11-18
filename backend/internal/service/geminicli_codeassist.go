package service

import (
	"context"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/geminicli"
)

// GeminiCliCodeAssistClient calls GeminiCli internal Code Assist endpoints.
type GeminiCliCodeAssistClient interface {
	LoadCodeAssist(ctx context.Context, accessToken, proxyURL string, req *geminicli.LoadCodeAssistRequest) (*geminicli.LoadCodeAssistResponse, error)
	OnboardUser(ctx context.Context, accessToken, proxyURL string, req *geminicli.OnboardUserRequest) (*geminicli.OnboardUserResponse, error)
}
