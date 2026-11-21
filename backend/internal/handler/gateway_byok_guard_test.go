package handler

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSharedGatewayRejectsTenantSelectorsBeforeSend(t *testing.T) {
	var upload bytes.Buffer
	form := multipart.NewWriter(&upload)
	require.NoError(t, form.WriteField("byok_credential_ref", "foreign"))
	require.NoError(t, form.Close())
	for _, tc := range []struct {
		name, header, value, query, content, body string
	}{
		{"funding header", "X-OmniHive-Funding-Source", "byok", "", "", ""},
		{"credential header", "X-BYOK-Credential-ID", "foreign", "", "", ""},
		{"funding query", "", "", "?allow_shared_fallback=false", "", ""},
		{"explicit source", "", "", "", "application/json", `{"funding_source":"byok"}`},
		{"cross workspace reference", "", "", "", "application/json", `{"byok_credential_id":"foreign"}`},
		{"contradictory duplicate", "", "", "", "application/json", `{"funding_source":"wallet","funding_source":"byok"}`},
		{"disabled fallback", "", "", "", "application/json", `{"allow_shared_fallback":false}`},
		{"multipart reference", "", "", "", form.FormDataContentType(), upload.String()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			router := gin.New()
			router.POST("/v1/embeddings", func(c *gin.Context) {
				if !prepareGatewayDispatch(c) {
					return
				}
				calls++
			})
			req := httptest.NewRequest(http.MethodPost, "/v1/embeddings"+tc.query, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", tc.content)
			if tc.header != "" {
				req.Header.Set(tc.header, tc.value)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			require.Equal(t, http.StatusForbidden, w.Code)
			require.Zero(t, calls)
		})
	}
}
