package requestmodel

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/tidwall/gjson"
)

// ValidateJSON rejects ambiguous routing fields before any model rewrite or
// decoding can erase them. Compare decoded keys case-insensitively because
// upstream struct decoders may accept case variants as the same field.
// Only routing objects are inspected: user tool schemas and message content
// can legitimately contain their own model properties.
func ValidateJSON(body []byte) error {
	if !json.Valid(body) || !gjson.ParseBytes(body).IsObject() {
		return errors.New("invalid JSON request body")
	}
	if !uniqueRoutingFields(gjson.ParseBytes(body), true) {
		return errors.New("duplicate model or session field")
	}
	return nil
}

func uniqueRoutingFields(object gjson.Result, inspectSession bool) bool {
	modelSeen, sessionSeen, valid := false, false, true
	object.ForEach(func(key, value gjson.Result) bool {
		switch {
		case strings.EqualFold(key.String(), "model"):
			valid = !modelSeen
			modelSeen = true
		case inspectSession && strings.EqualFold(key.String(), "session"):
			valid = !sessionSeen && (!value.IsObject() || uniqueRoutingFields(value, false))
			sessionSeen = true
		}
		return valid
	})
	return valid
}
