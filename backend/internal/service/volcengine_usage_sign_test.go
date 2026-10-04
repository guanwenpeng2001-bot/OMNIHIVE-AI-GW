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
		_, writeErr := m.Write([]byte(part))
		require.NoError(t, writeErr)
		key = m.Sum(nil)
	}
	m := hmac.New(sha256.New, key)
	_, writeErr := m.Write([]byte("HMAC-SHA256\n20240621T000000Z\n" + scope + "\n" + hex.EncodeToString(digest[:])))
	require.NoError(t, writeErr)
	require.Equal(t, "HMAC-SHA256 Credential=AKLTtest/"+scope+", SignedHeaders="+signed+", Signature="+hex.EncodeToString(m.Sum(nil)), req.Header.Get("Authorization"))
	require.Equal(t, query, req.URL.RawQuery)
	before := req.Header.Get("Authorization")
	signVolcengineUsage(req, []byte(`{"Scene":""}`), "AKLTtest", "secretkey", now)
	require.NotEqual(t, before, req.Header.Get("Authorization"))
}

func TestVolcengineAgentSignatureUsesTransmittedHostAndContentType(t *testing.T) {
	const query = "Action=GetSeatAFPUsage&Version=2024-01-01"
	const contentType = "application/json; charset=utf-8"
	body := []byte(`{"SeatIDs":["seat"]}`)
	request, err := http.NewRequest("POST", "https://ark.cn-beijing.volcengineapi.com/?"+query, nil)
	require.NoError(t, err)
	now := time.Date(2024, 6, 21, 0, 0, 0, 0, time.UTC)
	signVolcengineManagement(request, body, "AKLTtest", "secretkey", "cn-beijing", "ark", contentType, now)
	payloadDigest := sha256.Sum256(body)
	payloadHash := hex.EncodeToString(payloadDigest[:])
	const signed = "host;x-date;x-content-sha256;content-type"
	canonical := "POST\n/\n" + query + "\nhost:ark.cn-beijing.volcengineapi.com\nx-date:20240621T000000Z\nx-content-sha256:" + payloadHash + "\ncontent-type:application/json; charset=utf-8\n\n" + signed + "\n" + payloadHash
	canonicalDigest := sha256.Sum256([]byte(canonical))
	key := []byte("secretkey")
	for _, part := range []string{"20240621", "cn-beijing", "ark", "request"} {
		digest := hmac.New(sha256.New, key)
		_, writeErr := digest.Write([]byte(part))
		require.NoError(t, writeErr)
		key = digest.Sum(nil)
	}
	digest := hmac.New(sha256.New, key)
	_, writeErr := digest.Write([]byte("HMAC-SHA256\n20240621T000000Z\n20240621/cn-beijing/ark/request\n" + hex.EncodeToString(canonicalDigest[:])))
	require.NoError(t, writeErr)
	require.Equal(t, "HMAC-SHA256 Credential=AKLTtest/20240621/cn-beijing/ark/request, SignedHeaders="+signed+", Signature="+hex.EncodeToString(digest.Sum(nil)), request.Header.Get("Authorization"))
	require.Equal(t, contentType, request.Header.Get("Content-Type"))
	require.Equal(t, "ark.cn-beijing.volcengineapi.com", request.Host)
}

func TestArkSeatSignatureOfficialSDKVectors(test *testing.T) {
	// Fixed synthetic vectors independently calculated with Python hashlib/hmac,
	// using canonical/KDF steps inspected in both official Go SDKs.
	for _, scenario := range []struct{ region, host, signature string }{
		{"cn-beijing", "ark.cn-beijing.volcengineapi.com", "bc61b9a2c26b5f4b0a81a7ad7d602c6b66b645969b1812614d1255787404b37f"},
		{"ap-southeast-1", "ark.ap-southeast-1.byteplusapi.com", "9f4034466a6c851c493c208c2b105b037b378da803aa3d0943a6634e917cbd8d"},
	} {
		req, err := http.NewRequest("POST", "https://"+scenario.host+"/?Action=GetSeatInfoUsage&Version=2024-01-01", nil)
		require.NoError(test, err)
		signArkSeatUsage(req, []byte(`{"SeatID":"seat-1"}`), "AKLTtest", "secretkey", scenario.region, time.Date(2024, 6, 21, 0, 0, 0, 0, time.UTC))
		require.Equal(test, "HMAC-SHA256 Credential=AKLTtest/20240621/"+scenario.region+"/ark/request, SignedHeaders=host;x-content-sha256;x-date, Signature="+scenario.signature, req.Header.Get("Authorization"))
	}
}
