package domain

import "testing"

func TestOpenRouterMediaCellsRequireImplementedAdapters(t *testing.T) {
	for _, action := range []string{"image.generate", "audio.tts", "audio.stt", "embeddings", "rerank", "video.generate", "video.status", "video.content"} {
		cell := PlatformAction(ActionIdentity{Platform: PlatformOpenRouter, HostedAccountType: AccountTypeAPIKey}, action)
		if cell.Status != ActionImplemented || cell.Adapter == "" {
			t.Fatalf("missing %s adapter: %+v", action, cell)
		}
	}
	for _, action := range []string{"image.edit", "image.variation", "audio.translate", "video.edit", "video.extend", "video.cancel"} {
		if cell := PlatformAction(ActionIdentity{Platform: PlatformOpenRouter, HostedAccountType: AccountTypeAPIKey}, action); cell.Status == ActionImplemented {
			t.Fatalf("undocumented operation exposed: %+v", cell)
		}
	}
}
