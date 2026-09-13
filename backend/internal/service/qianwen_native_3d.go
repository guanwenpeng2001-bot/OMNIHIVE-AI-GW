package service

import "strings"

// QianwenNative3DPath is the official asynchronous Beijing workspace endpoint.
const QianwenNative3DPath = "/api/v1/services/aigc/video-generation/3d-generation"

const QianwenNative3DUnsupported = "Unsupported DashScope operation: 3D generation requires the dedicated asynchronous endpoint and a verified specification tariff"

// IsQianwenNative3DModel guards both official names (Tripo/Tripo-H3.1 and
// Tripo/Tripo-P1.0) and the legacy lowercase family, including mapped aliases.
// This is a rejection classifier, not an assertion that a model is supported.
func IsQianwenNative3DModel(model string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "tripo")
}
