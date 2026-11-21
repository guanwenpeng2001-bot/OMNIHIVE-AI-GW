package restore

import (
	"context"
	"io"
	"net/http"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/restoregate"
)

func (s *Service) Maintenance(next http.Handler) http.Handler {
	if s.Journal == nil {
		return next
	}
	gate := restoregate.New(s.Journal)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" && (r.Method == "GET" || r.Method == "HEAD") {
			next.ServeHTTP(w, r)
			return
		}
		ctx, leave, ok := gate.AdmitRequest(r.Context())
		if !ok {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Retry-After", "5")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, `{"code":503,"reason":"RESTORE_MAINTENANCE","message":"Production restore maintenance. Retry after recovery."}`)
			return
		}
		defer leave()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
func (s *Service) drainRequests(ctx context.Context) error {
	_, err := s.Journal.ExecContext(ctx, "SELECT pg_advisory_xact_lock(298,3)")
	return err
}
