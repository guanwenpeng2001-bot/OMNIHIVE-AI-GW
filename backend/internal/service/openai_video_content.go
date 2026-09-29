package service

import (
	"context"
	"net/http"
	"net/url"
)

// OpenOpenAIVideoContent streams one owner-resolved OpenAI video's content.
//
// Ownership is resolved by the ingress through the shared job service before
// this call; this adapter revalidates the account bound at admission and the
// opaque upstream id, so a disabled or re-typed account cannot keep serving
// stored content. The caller owns the response body.
func (s *OpenAIGatewayService) OpenOpenAIVideoContent(ctx context.Context, job *UpstreamJob) (*http.Response, error) {
	if job == nil || job.Platform != PlatformOpenAI || job.Family != "video" || !openAIResourceID(job.UpstreamTaskID) {
		return nil, ErrUpstreamJobNotFound
	}
	account, err := s.openAIResourceAccount(ctx, job)
	if err != nil {
		return nil, err
	}
	return s.openAIResourceStream(ctx, account, "/videos/"+url.PathEscape(job.UpstreamTaskID)+"/content")
}
