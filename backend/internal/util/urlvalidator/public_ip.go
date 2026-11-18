package urlvalidator

import (
	"net"
	"net/netip"
)

// IsPublicIP reports whether ip is an eligible public HTTP destination, not
// whether it is reachable from this machine. Invalid IPs fail closed. Unlike
// net.IP.IsGlobalUnicast/IsPrivate, this excludes non-public special-use space.
// Keep the policy and its IANA snapshots in docs/public-egress-policy.md in sync.
func IsPublicIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	return ok && isPublicAddr(addr)
}

func isPublicAddr(addr netip.Addr) bool {
	if !addr.IsValid() || addr.Zone() != "" {
		return false
	}
	addr = addr.Unmap()
	if addr.Is4() {
		// More-specific globally reachable assignments inside 192.0.0.0/24.
		if addr == pcpAnycast || addr == turnAnycast {
			return true
		}
		for _, prefix := range nonPublicIPv4 {
			if prefix.Contains(addr) {
				return false
			}
		}
		return true
	}
	if nat64WellKnown.Contains(addr) {
		// RFC 6052 section 3.1: a translator must not use the well-known
		// prefix for non-global IPv4. Enforce that here, not at the translator.
		bytes := addr.As16()
		return isPublicAddr(netip.AddrFrom4([4]byte(bytes[12:16])))
	}
	if ipv6Documentation.Contains(addr) {
		return false
	}
	for _, prefix := range publicIPv6 {
		if prefix.Contains(addr) {
			return true
		}
	}
	// Includes unallocated/reserved space, ULA, link/site-local, multicast,
	// local-use NAT64, Teredo and 6to4 (IANA global reachability is not True).
	return false
}

var (
	pcpAnycast        = netip.MustParseAddr("192.0.0.9")
	turnAnycast       = netip.MustParseAddr("192.0.0.10")
	nat64WellKnown    = netip.MustParsePrefix("64:ff9b::/96")
	ipv6Documentation = netip.MustParsePrefix("2001:db8::/32")
)

// IANA IPv4 Special-Purpose registry, updated 2025-10-09, reviewed 2026-11-18.
// Reject False, blank and N/A global reachability, plus multicast. Globally
// reachable AS112/AMT assignments outside these blocks remain eligible.
var nonPublicIPv4 = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
}

// IANA IPv6 Global Unicast assignments (2025-10-10) and Special-Purpose
// registry (2025-10-09), reviewed 2026-11-18. Only allocated public space and
// explicit globally reachable special assignments are eligible. New IANA
// allocations require review; do not replace this with all of 2000::/3.
var publicIPv6 = []netip.Prefix{
	// More-specific exceptions within the otherwise non-public 2001::/23.
	netip.MustParsePrefix("2001:1::1/128"),
	netip.MustParsePrefix("2001:1::2/128"),
	netip.MustParsePrefix("2001:1::3/128"),
	netip.MustParsePrefix("2001:3::/32"),
	netip.MustParsePrefix("2001:4:112::/48"),
	netip.MustParsePrefix("2001:20::/28"),
	netip.MustParsePrefix("2001:30::/28"),
	// RIR allocations; 2001:db8::/32 is excluded before this table.
	netip.MustParsePrefix("2001:200::/23"),
	netip.MustParsePrefix("2001:400::/23"),
	netip.MustParsePrefix("2001:600::/23"),
	netip.MustParsePrefix("2001:800::/22"),
	netip.MustParsePrefix("2001:c00::/23"),
	netip.MustParsePrefix("2001:e00::/23"),
	netip.MustParsePrefix("2001:1200::/23"),
	netip.MustParsePrefix("2001:1400::/22"),
	netip.MustParsePrefix("2001:1800::/23"),
	netip.MustParsePrefix("2001:1a00::/23"),
	netip.MustParsePrefix("2001:1c00::/22"),
	netip.MustParsePrefix("2001:2000::/19"),
	netip.MustParsePrefix("2001:4000::/23"),
	netip.MustParsePrefix("2001:4200::/23"),
	netip.MustParsePrefix("2001:4400::/23"),
	netip.MustParsePrefix("2001:4600::/23"),
	netip.MustParsePrefix("2001:4800::/23"),
	netip.MustParsePrefix("2001:4a00::/23"),
	netip.MustParsePrefix("2001:4c00::/23"),
	netip.MustParsePrefix("2001:5000::/20"),
	netip.MustParsePrefix("2001:8000::/19"),
	netip.MustParsePrefix("2001:a000::/20"),
	netip.MustParsePrefix("2001:b000::/20"),
	netip.MustParsePrefix("2003::/18"),
	netip.MustParsePrefix("2400::/12"),
	netip.MustParsePrefix("2410::/12"),
	netip.MustParsePrefix("2600::/12"),
	netip.MustParsePrefix("2610::/23"),
	netip.MustParsePrefix("2620::/23"),
	netip.MustParsePrefix("2630::/12"),
	netip.MustParsePrefix("2800::/12"),
	netip.MustParsePrefix("2a00::/12"),
	netip.MustParsePrefix("2a10::/12"),
	netip.MustParsePrefix("2c00::/12"),
}
