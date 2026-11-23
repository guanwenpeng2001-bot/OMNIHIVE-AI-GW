// Package modelpricing contains the offline LiteLLM baseline shipped with the binary.
package modelpricing

import _ "embed"

//go:embed model_prices_and_context_window.json
var catalog string

//go:embed cache-base-overrides.json
var cacheBaseOverrides string

//go:embed provider-tariff-bindings.json
var providerTariffBindings string

// ProviderTariffBindingsJSON returns the reviewed legacy and hosted tariff bindings.
func ProviderTariffBindingsJSON() []byte { return []byte(providerTariffBindings) }

// JSON returns an independent copy of the packaged baseline.
func JSON() []byte { return []byte(catalog) }

// CacheBaseOverridesJSON returns an independent copy of the default cache repairs.
func CacheBaseOverridesJSON() []byte { return []byte(cacheBaseOverrides) }
