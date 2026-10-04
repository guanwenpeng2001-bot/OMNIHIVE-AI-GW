package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"
)

// Algorithm and signed-header order follow volcengine/volc-openapi-demos
// signature/golang/sign.go. This signs the exact transmitted URL and JSON bytes.
func signVolcengineUsage(req *http.Request, body []byte, accessKey, secretKey string, now time.Time) {
	signVolcengineManagement(req, body, accessKey, secretKey, "cn-beijing", "ark", "application/json", now)
}

func signVolcengineManagement(req *http.Request, body []byte, accessKey, secretKey, region, service, contentType string, now time.Time) {
	hash := func(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
	mac := func(key []byte, data string) []byte {
		h := hmac.New(sha256.New, key)
		_, _ = h.Write([]byte(data))
		return h.Sum(nil)
	}
	date := now.UTC().Format("20060102T150405Z")
	payload := hash(body)
	req.Header.Set("X-Date", date)
	req.Header.Set("X-Content-Sha256", payload)
	req.Header.Set("Content-Type", contentType)
	const headers = "host;x-date;x-content-sha256;content-type"
	canonical := strings.Join([]string{req.Method, req.URL.EscapedPath(), req.URL.RawQuery,
		"host:" + req.URL.Host + "\nx-date:" + date + "\nx-content-sha256:" + payload + "\ncontent-type:" + contentType + "\n", headers, payload}, "\n")
	scope := date[:8] + "/" + region + "/" + service + "/request"
	key := mac(mac(mac(mac([]byte(secretKey), date[:8]), region), service), "request")
	signature := hex.EncodeToString(mac(key, "HMAC-SHA256\n"+date+"\n"+scope+"\n"+hash([]byte(canonical))))
	req.Header.Set("Authorization", "HMAC-SHA256 Credential="+accessKey+"/"+scope+", SignedHeaders="+headers+", Signature="+signature)
}
