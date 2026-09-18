// Package modelcapabilities contains reviewed capability supplements, never prices.
package modelcapabilities

import (
	_ "embed"
	"sync"
)

//go:embed catalog.json
var catalog string

var verifyOnce sync.Once
var verifyError error

// JSON returns only the compiled, trusted release. Observations are never loaded
// from a working directory or promoted to approval by their content hash.
func JSON() []byte {
	verifyOnce.Do(func() {
		_, verifyError = VerifyRelease([]byte(catalog), []byte(releaseManifest), []byte(releasePin), nil)
	})
	if verifyError != nil {
		panic("invalid compiled capability release: " + verifyError.Error())
	}
	return []byte(catalog)
}
