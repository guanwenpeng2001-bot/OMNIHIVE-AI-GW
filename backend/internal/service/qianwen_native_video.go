package service

import (
	"fmt"

	"github.com/tidwall/gjson"
)

// Video generation and editing share this official asynchronous endpoint.
// Do not register it as a conversation operation: accepting a task requires
// durable tenant/account ownership and terminal, idempotent per-second billing.
// In particular, the adjacent 3d-generation endpoint is a different family.
const QianwenNativeVideoPath = "/api/v1/services/aigc/video-generation/video-synthesis"

// rejectQianwenNativeAsyncResponse prevents an accepted but unowned job from
// being returned as a successful synchronous conversation with zero token usage.
// This check also covers a configured text alias unexpectedly returning a video
// task. Presence, even with an empty/null value, is a protocol mismatch.
func rejectQianwenNativeAsyncResponse(body []byte) error {
	for _, field := range []string{"output.task_id", "output.task_status"} {
		if gjson.GetBytes(body, field).Exists() {
			return fmt.Errorf("unsupported DashScope asynchronous task in synchronous response: %s", field)
		}
	}
	return nil
}
