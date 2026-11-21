package modelcapabilities

import _ "embed"

//go:embed pi-supplement.json
var piSupplement string

// PiSupplementJSON returns a copy of the dependency catalog, which is
// independent of the reviewed release.
func PiSupplementJSON() []byte { return []byte(piSupplement) }
