package modelpricing

import _ "embed"

//go:embed pi-prices-supplement.json
var piSupplement string

// PiSupplementJSON returns a copy of the dependency's reference token prices.
func PiSupplementJSON() []byte { return []byte(piSupplement) }
