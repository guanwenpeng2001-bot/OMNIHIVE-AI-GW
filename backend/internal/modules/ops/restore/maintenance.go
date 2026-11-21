package restore

import (
	"context"
	"fmt"
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
		ctx, leave, err := gate.Admit(r.Context())
		if err != nil {
			reason := "RESTORE_MAINTENANCE"
			message := "Production restore maintenance. Retry after recovery."
			if restoregate.IsUnavailable(err) {
				reason = "RESTORE_STATUS_UNAVAILABLE"
				message = "Restore status is temporarily unavailable. Retry later."
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = fmt.Fprintf(w, `{"code":503,"reason":%q,"message":%q}`, reason, message)
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
