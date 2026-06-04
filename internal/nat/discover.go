package nat

import (
	"fmt"
	"net"
	"strconv"
)

// DiscoverLocalAddrs returns IPv4 "host:port" candidates the local machine
// is reachable at from the LAN. Filters out:
//   - loopback (caller adds 127.0.0.1 explicitly if needed)
//   - link-local (169.254.0.0/16) — typically not useful for transfers
//   - IPv6 (deferred; see note below)
//
// IPv6 is skipped for step 4 — supporting it well means dealing with
// link-local scopes, ULA prefixes, and dual-stack candidate ordering. All
// worth doing eventually, not in scope here.
func DiscoverLocalAddrs(port int) ([]string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, fmt.Errorf("interface addrs: %w", err)
	}

	var result []string
	seen := map[string]bool{}

	for _, a := range addrs {
		ipNet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipNet.IP.To4()
		if ip == nil {
			continue
		}
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			continue
		}
		s := ip.String()
		if seen[s] {
			continue
		}
		seen[s] = true
		result = append(result, net.JoinHostPort(s, strconv.Itoa(port)))
	}
	return result, nil
}
