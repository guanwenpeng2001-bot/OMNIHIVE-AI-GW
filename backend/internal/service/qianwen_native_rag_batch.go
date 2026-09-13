package service

import "strings"

// QianwenNativeResourceGate describes a disabled resource API. Resource IDs
// cannot be routed like models: every read, download and mutation needs durable
// tenant/key/account ownership before it may reach a provider.
type QianwenNativeResourceGate struct {
	Family string
	Code   string
	Reason string
}

// QianwenNativeResourceGateForPath intentionally rejects entire resource trees,
// including unknown actions and repeated polls. No job is created and no usage
// is fabricated while ownership and exactly-once settlement are unavailable.
// The compatible paths are recognized for rejection, never native aliases.
func QianwenNativeResourceGateForPath(path string) (QianwenNativeResourceGate, bool) {
	for _, entry := range []struct {
		prefix string
		gate   QianwenNativeResourceGate
	}{
		{"/api/v1/apps", QianwenNativeResourceGate{"rag", "qianwen_rag_unsupported", "application/session ownership and per-model retrieval billing are not implemented"}},
		{"/api/v1/indices", QianwenNativeResourceGate{"rag", "qianwen_rag_unsupported", "knowledge retrieval ownership and pricing are not implemented; knowledge-base control APIs require separate RAM authorization"}},
		{"/api/v1/services/aigc/rag", QianwenNativeResourceGate{"rag", "qianwen_rag_unsupported", "RAG is not a conversation generation operation"}},
		{"/api/v1/files", QianwenNativeResourceGate{"files", "qianwen_files_unsupported", "files use the official compatible-mode API; tenant/account ownership, storage policy and bounded transfers are not implemented"}},
		{"/compatible-mode/v1/files", QianwenNativeResourceGate{"files", "qianwen_files_unsupported", "file upload, list, metadata, content and deletion require tenant/account ownership and storage policy"}},
		{"/api/v1/services/aigc/files", QianwenNativeResourceGate{"files", "qianwen_files_unsupported", "files are not a native generation operation"}},
		{"/api/v1/batches", QianwenNativeResourceGate{"batch", "qianwen_batch_unsupported", "file batches use the official compatible-mode API; persistent jobs and per-item idempotent settlement are not implemented"}},
		{"/compatible-mode/v1/batches", QianwenNativeResourceGate{"batch", "qianwen_batch_unsupported", "batch submit, list, poll, cancel and results require owned files, restart recovery and per-custom_id settlement using file-batch prices; synchronous Batch Chat prices do not apply"}},
		{"/api/v1/services/aigc/batch", QianwenNativeResourceGate{"batch", "qianwen_batch_unsupported", "file batches are not a native generation operation"}},
	} {
		if path == entry.prefix || strings.HasPrefix(path, entry.prefix+"/") {
			return entry.gate, true
		}
	}
	return QianwenNativeResourceGate{}, false
}
