package nat

import (
	"net"
	"strconv"
)

// DiscoverLocalAddrs returns non-loopback IPv4 addresses on local interfaces,
// formatted as IP:port. Used to share LAN-reachable candidates with the peer
// for the case where both peers are on the same network.
//
// Note: the relay's view of the WebSocket connection's RemoteAddr gives you
// your public IP:port for the TCP socket. For UDP hole punching you'll need
// a separate UDP probe to a UDP endpoint on the relay — that's outside the
// scope of this function.
//
// TODO: implement using net.Interfaces() + net.InterfaceAddrs(). Filter out
// loopback, link-local (169.254.0.0/16), and disabled interfaces.
func DiscoverLocalAddrs(port int) ([]string, error) {
	_ = strconv.Itoa
	return nil, nil
}

// FormatAddr is a small helper to build "host:port" strings safely (handles IPv6 brackets).
func FormatAddr(host string, port int) string {
	return net.JoinHostPort(host, strconv.Itoa(port))
}
