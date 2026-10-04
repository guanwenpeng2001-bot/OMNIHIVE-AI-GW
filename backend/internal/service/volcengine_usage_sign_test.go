package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestVolcengineSignatureOfficialAlgorithmVector(t *testing.T) {
	// Synthetic fixed input; independently construct the published canonical
	// material instead of accepting a signature copied from another client.
	const query = "Action=GetCodingPlanUsage&Version=2024-01-01"
	req, err := http.NewRequest("POST", volcengineUsageEndpoint+"?"+query, strings.NewReader(`{}`))
	require.NoError(t, err)
	now := time.Date(2024, 6, 21, 0, 0, 0, 0, time.UTC)
	signVolcengineUsage(req, []byte(`{}`), "AKLTtest", "secretkey", now)
	const payloadHash = "44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a"
	require.Equal(t, payloadHash, req.Header.Get("X-Content-Sha256"))
	require.Equal(t, "20240621T000000Z", req.Header.Get("X-Date"))
	const scope = "20240621/cn-beijing/ark/request"
	const signed = "host;x-date;x-content-sha256;content-type"
	canonical := "POST\n/\n" + query + "\nhost:open.volcengineapi.com\nx-date:20240621T000000Z\nx-content-sha256:" + payloadHash + "\ncontent-type:application/json\n\n" + signed + "\n" + payloadHash
	digest := sha256.Sum256([]byte(canonical))
	key := []byte("secretkey")
	for _, part := range []string{"20240621", "cn-beijing", "ark", "request"} {
		m := hmac.New(sha256.New, key)
		m.Write([]byte(part))
		key = m.Sum(nil)
	}
	m := hmac.New(sha256.New, key)
	m.Write([]byte("HMAC-SHA256\n20240621T000000Z\n" + scope + "\n" + hex.EncodeToString(digest[:])))
	require.Equal(t, "HMAC-SHA256 Credential=AKLTtest/"+scope+", SignedHeaders="+signed+", Signature="+hex.EncodeToString(m.Sum(nil)), req.Header.Get("Authorization"))
	require.Equal(t, query, req.URL.RawQuery)
	before := req.Header.Get("Authorization")
	signVolcengineUsage(req, []byte(`{"Scene":""}`), "AKLTtest", "secretkey", now)
	require.NotEqual(t, before, req.Header.Get("Authorization"))
}
