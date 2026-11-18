#!/usr/bin/env bash
set -euo pipefail
# Pass an absolute path to a Linux httpclient test binary. No host DNS, routes,
# interfaces, services or production configuration are changed.
binary=$(realpath "${1:?usage: test-public-egress-netns.sh /path/to/httpclient.test}")
exec unshare --user --map-root-user --net -- bash -euo pipefail -c '
  ip link set lo up
  for addr in 93.184.216.34 192.0.0.9 198.18.0.1 198.19.0.1 100.64.0.1 203.0.113.1 10.1.2.3; do
    ip address add "$addr/32" dev lo
  done
  for addr in 2606:4700:4700::1111 2001:3::1 64:ff9b::808:808 2001:db8::1 3fff::1 64:ff9b::7f00:1; do
    ip -6 address add "$addr/128" dev lo nodad
  done
  export OMNIHIVE_SSRF_NETNS_TEST=1
  exec "$1" -test.run "^TestPublicEgressNetworkNamespace$" -test.v -test.timeout 60s
' bash "$binary"
