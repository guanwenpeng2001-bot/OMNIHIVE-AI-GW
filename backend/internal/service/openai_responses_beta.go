package service

import (
	"net/http"
	"strings"
)

// preserveRequestedResponsesBeta opts in to the supported Responses token
// without admitting arbitrary caller beta headers into the normal forwarder.
func preserveRequestedResponsesBeta(source, target http.Header) {
	for _, value := range source.Values("OpenAI-Beta") {
		for _, token := range strings.Split(value, ",") {
			if strings.TrimSpace(token) == "responses_multi_agent=v1" {
				target.Set("OpenAI-Beta", "responses_multi_agent=v1")
				return
			}
		}
	}
}
