package requestmodel

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestValidateJSONRoutingFields(t *testing.T) {
	for _, body := range []string{
		`{"model":"allowed","model":"revoked"}`,
		`{"model":"allowed","model":"allowed"}`,
		`{"model":"allowed","MODEL":null}`,
		`{"model":"allowed","mo\u0064el":"revoked"}`,
		`{"model":"allowed","session":{"model":"a","model":"b"}}`,
		`{"session":{"model":"a"},"SESSION":{"model":"b"}}`,
		`[]`, `null`, `{"model":"a"} {"model":"b"}`,
	} {
		t.Run(body, func(t *testing.T) { require.Error(t, ValidateJSON([]byte(body))) })
	}
	for _, body := range []string{
		`{"model":"allowed"}`, `{"type":"response.create"}`,
		`{"session":{"model":"allowed"}}`,
		`{"model":"allowed","messages":[{"content":"model"}],"tools":[{"properties":{"model":{"type":"string"}}}]}`,
	} {
		t.Run(body, func(t *testing.T) { require.NoError(t, ValidateJSON([]byte(body))) })
	}
}
