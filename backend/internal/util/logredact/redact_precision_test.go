package logredact

import (
	"strings"
	"testing"
)

func TestRedactJSONPreservesNumbers(t *testing.T) {
	raw := `{"id":9007199254740993,"amount":0.1234567890123456789,"token":"secret","nested":[9223372036854775807]}`
	for _, out := range []string{RedactJSON([]byte(raw)), RedactText(raw)} {
		for _, number := range []string{"9007199254740993", "0.1234567890123456789", "9223372036854775807"} {
			if !strings.Contains(out, number) {
				t.Errorf("lost exact number %s in %s", number, out)
			}
		}
		if strings.Contains(out, "secret") {
			t.Error("secret was not redacted")
		}
	}
}

func TestRedactJSONRejectsTrailingDocuments(t *testing.T) {
	for _, raw := range []string{`{"id":1} {"token":"secret"}`, `{"id":1} garbage`} {
		if got := RedactJSON([]byte(raw)); got != "<non-json payload redacted>" {
			t.Errorf("accepted invalid JSON: %s", got)
		}
	}
}
