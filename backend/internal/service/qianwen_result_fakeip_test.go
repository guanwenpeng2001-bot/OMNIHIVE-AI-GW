//go:build unit

package service

import (
	"net"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
	"github.com/stretchr/testify/require"
)

func TestQianwenResultFakeIPRecognition(t *testing.T) {
	for _, address := range []string{
		"198.18.0.0", "198.18.255.255", "198.19.0.1", "198.19.255.255",
		"::ffff:198.18.0.1", "::ffff:198.19.255.255",
		"2001:2::", "2001:2::abcd", "2001:2:0:ffff:ffff:ffff:ffff:ffff",
		"fdfe:dcba:9876::7",
	} {
		t.Run(address, func(t *testing.T) {
			ip := net.ParseIP(address)
			require.True(t, isQianwenResultFakeIP(ip))
			require.False(t, urlvalidator.IsPublicIP(ip), "a fallback trigger must never become a public destination")
		})
	}
	for _, address := range []string{
		"198.17.255.255", "198.20.0.0", "2001:1:ffff:ffff:ffff:ffff:ffff:ffff", "2001:2:1::",
		"fdfe:dcba:9876:1::", "10.0.0.1", "100.64.0.1", "203.0.113.1", "64:ff9b::c613:1", "8.8.8.8",
	} {
		require.False(t, isQianwenResultFakeIP(net.ParseIP(address)), address)
	}
	for _, invalid := range []net.IP{nil, {}, {1, 2, 3, 4, 5}} {
		require.False(t, isQianwenResultFakeIP(invalid))
	}
}
