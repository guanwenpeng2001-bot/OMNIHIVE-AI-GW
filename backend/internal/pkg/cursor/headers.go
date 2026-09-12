package cursor

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"runtime"
	"time"

	"github.com/google/uuid"
)

const DefaultClientVersion = "3.16.17"

type Credentials struct{ AccessToken, MachineID, MacMachineID, ClientVersion string }

// Checksum follows Cursor's rolling XOR timestamp encoding, using stable account IDs.
func Checksum(machineID, macMachineID string, now time.Time) string {
	timestamp := uint64(now.UnixMilli() / 1_000_000)
	encoded := make([]byte, 6)
	previous := byte(165)
	for index := range encoded {
		encoded[index] = (byte(timestamp>>uint(8*(5-index))) ^ previous) + byte(index)
		previous = encoded[index]
	}
	return base64.RawURLEncoding.EncodeToString(encoded) + machineID + "/" + macMachineID
}

func headers(credentials Credentials, runID string) http.Header {
	return headersAt(credentials, runID, time.Now(), uuid.NewString())
}

func headersAt(credentials Credentials, runID string, now time.Time, requestID string) http.Header {
	version := credentials.ClientVersion
	if version == "" {
		version = DefaultClientVersion
	}
	platform, architecture := runtime.GOOS, runtime.GOARCH
	if platform == "windows" {
		platform = "win32"
	}
	if architecture == "amd64" {
		architecture = "x64"
	}
	values := map[string]string{
		"Authorization":                "Bearer " + credentials.AccessToken,
		"Content-Type":                 "application/connect+proto",
		"Connect-Protocol-Version":     "1",
		"Connect-Accept-Encoding":      "gzip",
		"User-Agent":                   "connect-es/1.6.1",
		"X-Client-Key":                 fmt.Sprintf("%x", sha256.Sum256([]byte(credentials.AccessToken))),
		"X-Cursor-Checksum":            Checksum(credentials.MachineID, credentials.MacMachineID, now),
		"X-Cursor-Client-Version":      version,
		"X-Cursor-Client-Type":         "cli",
		"X-Cursor-Client-Layout":       "unifiedAgent",
		"X-Cursor-Client-Os":           platform,
		"X-Cursor-Client-Arch":         architecture,
		"X-Cursor-Client-Device-Type":  "desktop",
		"X-Cursor-Timezone":            "UTC",
		"X-Cursor-Agent-Allowed-Tools": "mcp_tool_call",
		"X-Ghost-Mode":                 "true",
		"X-Request-Id":                 requestID,
		"X-Original-Request-Id":        runID,
		"X-Amzn-Trace-Id":              "Root=" + requestID,
	}
	result := make(http.Header)
	for name, value := range values {
		result.Set(name, value)
	}
	return result
}
