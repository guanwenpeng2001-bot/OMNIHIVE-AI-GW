package service

import "github.com/tidwall/gjson"

func gatewayFailoverCanReplay(failure *UpstreamFailoverError) bool {
	if failure == nil || failure.NextAccountAction == NextAccountStop {
		return false
	}
	// These codes describe local read failures after submission, not a provider
	// rejection. A buffered adapter has not written downstream, but side effects
	// are still unknown and changing accounts would duplicate the request.
	switch gjson.GetBytes(failure.ResponseBody, "error.code").String() {
	case OpenAIUpstreamHTTP2StreamErrorCode, OpenAIUpstreamStreamReadErrorCode, OpenAIUpstreamStreamTruncatedCode:
		return false
	}
	return true
}
