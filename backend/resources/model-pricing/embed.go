// Package modelpricing contains the offline LiteLLM baseline shipped with the binary.
package modelpricing

import _ "embed"

//go:embed model_prices_and_context_window.json
var catalog string

// JSON returns an independent copy of the packaged baseline.
func JSON() []byte { return []byte(catalog) }
