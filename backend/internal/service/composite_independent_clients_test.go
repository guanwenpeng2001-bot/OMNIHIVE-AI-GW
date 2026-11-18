//go:build unit

package service_test

import (
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil"
)

func TestCompositeGroupIndependentClients(test *testing.T) {
	testutil.RunCompositeGroupIndependentClients(test)
}
