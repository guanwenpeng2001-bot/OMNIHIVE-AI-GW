// Package modelcapabilities contains reviewed capability supplements, never prices.
package modelcapabilities

import (
	_ "embed"
	"strings"
	"sync"
)

//go:embed catalog.json
var catalog string

var verifyOnce sync.Once
var verifyError error

// JSON returns only the compiled, trusted release. Observations are never loaded
// from a working directory or promoted to approval by their content hash.
func JSON() []byte {
	// Git may check out reviewed JSON as CRLF on Windows. The frozen tuple
	// authenticates its canonical LF bytes, independent of checkout settings.
	body := []byte(strings.ReplaceAll(catalog, "\r\n", "\n"))
	verifyOnce.Do(func() {
		_, verifyError = VerifyRelease(body, ManifestJSON(), []byte(releasePin), nil)
	})
	if verifyError != nil {
		panic("invalid compiled capability release: " + verifyError.Error())
	}
	return body
}
