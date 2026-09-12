//go:build unit

package service

import "github.com/Wei-Shaw/sub2api/internal/pkg/cursor"

// NewCursorGatewayWithClientForTest lets route tests use the real wire driver
// against a local HTTP/2 server without changing production endpoints.
func NewCursorGatewayWithClientForTest(cache CursorSessionCache, client *cursor.Client) *CursorGatewayService {
	gateway := NewCursorGatewayService(NewCursorSessionProvider(cache), nil)
	gateway.native = client
	return gateway
}
