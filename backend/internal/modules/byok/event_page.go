package byok

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
)

type eventSeek struct {
	After int64
	AsOf  time.Time
}

func (s *Store) eventMAC(t Target, data []byte) []byte {
	h := hmac.New(sha256.New, s.signing)
	fmt.Fprintf(h, "byok/events/v1/%d/%d/%d/%d\x00", t.Workspace, t.Project, t.ID, t.Actor)
	_, _ = h.Write(data)
	return h.Sum(nil)
}
func (s *Store) eventCursor(t Target, p eventSeek) (string, error) {
	if len(s.signing) == 0 {
		return "", errors.New("missing BYOK pagination key")
	}
	b, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b) + "." + base64.RawURLEncoding.EncodeToString(s.eventMAC(t, b)), nil
}
func (s *Store) eventPage(t Target, cursor string) (eventSeek, error) {
	p := eventSeek{AsOf: time.Now().UTC()}
	if cursor == "" {
		return p, nil
	}
	if len(s.signing) == 0 || len(cursor) > 512 {
		return p, fault.Invalid("/cursor")
	}
	payload, tag, ok := strings.Cut(cursor, ".")
	b, bodyErr := base64.RawURLEncoding.DecodeString(payload)
	mac, tagErr := base64.RawURLEncoding.DecodeString(tag)
	if !ok || bodyErr != nil || tagErr != nil || !hmac.Equal(mac, s.eventMAC(t, b)) || json.Unmarshal(b, &p) != nil || p.After < 1 {
		return p, fault.Invalid("/cursor")
	}
	if time.Since(p.AsOf) > 15*time.Minute {
		return p, &fault.Fault{Status: 410, Reason: "SNAPSHOT_EXPIRED", Message: "The lifecycle event cursor has expired."}
	}
	return p, nil
}
