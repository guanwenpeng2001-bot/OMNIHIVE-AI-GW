//go:build unit

package service_test

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/testutil"
)

func TestCompositeGroupIndependentClients(test *testing.T) {
	testutil.RunCompositeGroupIndependentClients(test)
}
