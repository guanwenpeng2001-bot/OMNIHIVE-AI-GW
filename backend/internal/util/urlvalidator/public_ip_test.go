package urlvalidator

import (
	"context"
	"encoding/csv"
	"net"
	"net/netip"
	"os"
	"strings"
	"testing"
)

func TestPublicIPPolicy(t *testing.T) {
	for _, tc := range []struct {
		public bool
		ips    string
	}{
		{true, "1.1.1.1 8.8.8.8 93.184.216.34 100.63.255.255 100.128.0.0 198.17.255.255 198.20.0.0 192.0.0.9 192.0.0.10 192.31.196.1 192.52.193.1 192.175.48.1"},
		{true, "2606:4700:4700::1111 2001:4860:4860::8888 2410::1 2001:1::1 2001:1::2 2001:1::3 2001:3::1 2001:4:112::1 2001:20::1 2001:30::1 2620:4f:8000::1"},
		{true, "::ffff:8.8.8.8 ::ffff:192.0.0.9 64:ff9b::808:808 64:ff9b::c000:9"},
		{false, "0.1.2.3 10.1.2.3 100.64.0.0 100.127.255.255 127.1.2.3 169.254.169.254 172.16.0.1 192.0.0.8 192.0.0.11 192.0.0.170 192.0.2.1 192.88.99.1 192.88.99.2 192.168.1.1 198.18.0.0 198.19.255.255 198.51.100.1 203.0.113.1 224.0.0.1 239.1.2.3 240.0.0.0 255.255.255.255"},
		{false, ":: ::1 ::8.8.8.8 ::ffff:198.19.1.1 ::ffff:100.64.0.1 64:ff9b::a00:1 64:ff9b::7f00:1 64:ff9b::c612:1 64:ff9b::6440:1 64:ff9b::c000:201 64:ff9b:1::808:808"},
		{false, "100::1 100:0:0:1::1 2001::1 2001:1::4 2001:2::1 2001:10::1 2001:db8::1 2002:808:808::1 2002:7f00:1::1 3ffe::1 3fff::1 5f00::1 fc00::1 fe80::1 fec0::1 ff02::1 ff0e::1 4000::1 2000::1 2001:1000::1 3000::1"},
	} {
		for _, value := range strings.Fields(tc.ips) {
			t.Run(value, func(t *testing.T) {
				ip := net.ParseIP(value)
				if ip == nil {
					t.Fatal("invalid fixture")
				}
				if IsPublicIP(ip) != tc.public || IsBlockedHost(value) == tc.public {
					t.Fatalf("public=%v expected %v", IsPublicIP(ip), tc.public)
				}
				if ip4 := ip.To4(); ip4 != nil && IsPublicIP(ip4) != tc.public {
					t.Fatal("four-byte and mapped IPv4 policy diverged")
				}
				_, err := ValidateHTTPURL("https://"+net.JoinHostPort(value, "443"), false, ValidationOptions{})
				if (err == nil) != tc.public {
					t.Fatalf("URL policy diverged: %v", err)
				}
			})
		}
	}
	for _, ip := range []net.IP{nil, {}, {1}, {1, 2, 3, 4, 5}, make(net.IP, 17)} {
		if IsPublicIP(ip) {
			t.Fatalf("malformed IP accepted: %v", ip)
		}
	}
	for _, host := range []string{"LOCALHOST.", "sub.localhost.", "fe80::1%eth0", "2606:4700::1%eth0", "::ffff:8.8.8.8%eth0", "bad::ip"} {
		if !IsBlockedHost(host) {
			t.Fatalf("scoped/local/malformed host accepted: %s", host)
		}
	}
}

func TestPublicIPPreservesExplicitPrivateAndAllowlistPolicy(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "10.0.0.1", "198.18.1.1", "100.64.0.1", "fc00::1", "localhost"} {
		raw := "https://" + net.JoinHostPort(host, "443")
		opts := ValidationOptions{AllowPrivate: true, RequireAllowlist: true, AllowedHosts: []string{host}}
		if _, err := ValidateHTTPSURL(raw, opts); err != nil {
			t.Fatal(err)
		}
		opts.AllowPrivate = false
		if _, err := ValidateHTTPSURL(raw, opts); err == nil {
			t.Fatal("allowlist alone must not override public policy")
		}
		opts.AllowPrivate = true
		opts.AllowedHosts = []string{"other.example"}
		if _, err := ValidateHTTPSURL(raw, opts); err == nil {
			t.Fatal("private permission must not override host allowlist")
		}
		if _, err := ValidateURLFormat(raw, false); err != nil {
			t.Fatal("format-only policy changed")
		}
	}
}

func TestValidateResolvedIPSpecialLiterals(t *testing.T) {
	for _, host := range []string{"198.18.1.1", "198.19.1.1", "100.64.0.1", "203.0.113.1", "::ffff:198.18.1.1", "64:ff9b::7f00:1", "localhost."} {
		if err := ValidateResolvedIP(context.Background(), host); err == nil {
			t.Fatalf("accepted %s", host)
		}
	}
	for _, host := range []string{"8.8.8.8", "192.0.0.9", "2001:3::1", "64:ff9b::808:808"} {
		if err := ValidateResolvedIP(context.Background(), host); err != nil {
			t.Fatal(err)
		}
	}
}

// Use checked-in official data as an independent oracle, not the production
// prefix lists. Test both ends of every allocation and longest-prefix overrides.
func TestPublicIPAgainstIANASnapshots(t *testing.T) {
	type rule struct {
		prefix netip.Prefix
		public bool
	}
	var rules []rule
	for _, filename := range []string{"iana-ipv4-special.csv", "iana-ipv6-special.csv", "iana-ipv6-unicast.csv"} {
		file, err := os.Open("testdata/" + filename)
		if err != nil {
			t.Fatal(err)
		}
		rows, err := csv.NewReader(file).ReadAll()
		_ = file.Close()
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows[1:] {
			var public bool
			if filename == "iana-ipv6-unicast.csv" {
				public = row[5] == "ALLOCATED"
			} else {
				public = strings.TrimSpace(row[8]) == "True"
			}
			for _, value := range strings.Split(row[0], ",") {
				prefix, err := netip.ParsePrefix(strings.Fields(value)[0])
				if err != nil {
					t.Fatal(err)
				}
				rules = append(rules, rule{prefix, public})
			}
		}
	}
	for _, entry := range rules {
		first := entry.prefix.Masked().Addr()
		bytes := first.As16()
		start := entry.prefix.Bits()
		if first.Is4() {
			start += 96
		}
		for bit := start; bit < 128; bit++ {
			bytes[bit/8] |= 1 << (7 - bit%8)
		}
		last := netip.AddrFrom16(bytes).Unmap()
		for _, addr := range []netip.Addr{first, first.Next(), last} {
			if !entry.prefix.Contains(addr) {
				continue
			}
			want, bits := addr.Is4(), -1
			for _, candidate := range rules {
				if candidate.prefix.Contains(addr) && candidate.prefix.Bits() > bits {
					want, bits = candidate.public, candidate.prefix.Bits()
				}
			}
			// Mapped IPs follow their underlying IPv4 policy, and the NAT64
			// well-known prefix additionally requires a public embedded IPv4.
			if addr.Is4In6() || netip.MustParsePrefix("64:ff9b::/96").Contains(addr) {
				continue
			}
			if got := IsPublicIP(net.IP(addr.AsSlice())); got != want {
				t.Errorf("IANA %s sample %s: public=%v, want %v", entry.prefix, addr, got, want)
			}
		}
	}
}
