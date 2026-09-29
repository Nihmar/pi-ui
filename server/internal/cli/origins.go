package cli

import (
	"net"
	"sort"
	"strconv"
	"strings"
)

// defaultListenPort is the port a server binds and a card advertises when nobody said
// otherwise; it mirrors defaultAddr.
const defaultListenPort = 8787

// systemAddrs returns the host's interface addresses. It is the seam the origin detection
// takes as a parameter, so tests drive it with fake addresses.
func systemAddrs() []net.Addr {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	return addrs
}

// advertiseOrigins returns the origins a client can dial for a server listening on
// bindAddr:port, plus whether the bind host is loopback-only: nothing outside this host can
// reach a loopback bind, which is what the serve banner warns about.
//
// The host of bindAddr decides: empty, 0.0.0.0 and :: mean every interface (the detected
// candidates), a loopback host means this host only, anything else is one origin.
func advertiseOrigins(bindAddr string, port int, scheme string, addrs []net.Addr) ([]string, bool) {
	host := addrHost(bindAddr)
	switch {
	case host == "" || host == "0.0.0.0" || host == "::":
		return originsFrom(addrs, scheme, port), false
	case isLoopbackHost(host):
		return []string{originURL(scheme, host, port)}, true
	default:
		return []string{originURL(scheme, host, port)}, false
	}
}

// originsFrom turns interface addresses into dialable origins: IPv4 only, because a phone on
// a LAN dials the IPv4 address and the loopback, link-local and multicast entries are noise.
// Private ranges come first, then CGNAT/Tailscale, then the rest, and the order is stable so
// the first origin — the one the link carries — is the same on every run.
func originsFrom(addrs []net.Addr, scheme string, port int) []string {
	type candidate struct {
		origin string
		rank   int
	}
	seen := map[string]bool{}
	candidates := make([]candidate, 0, len(addrs))
	for _, addr := range addrs {
		ip := addrIP(addr)
		if ip == nil || ip.To4() == nil {
			continue
		}
		ip = ip.To4()
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsMulticast() {
			continue
		}
		origin := originURL(scheme, ip.String(), port)
		if seen[origin] {
			continue
		}
		seen[origin] = true
		candidates = append(candidates, candidate{origin: origin, rank: addressRank(ip)})
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].rank < candidates[j].rank })

	origins := make([]string, 0, len(candidates))
	for _, entry := range candidates {
		origins = append(origins, entry.origin)
	}
	return origins
}

// addressRank orders the detected addresses: a home or office LAN first, then the 172.16/12
// range containers use by default, then Tailscale's 100.64/10, then anything else.
func addressRank(ip net.IP) int {
	switch {
	case ip[0] == 192 && ip[1] == 168, ip[0] == 10:
		return 0
	case ip[0] == 172 && ip[1] >= 16 && ip[1] <= 31:
		return 1
	case ip[0] == 100 && ip[1] >= 64 && ip[1] <= 127:
		return 2
	default:
		return 3
	}
}

// originURL formats one origin. The host is joined with the port, so an IPv6 literal stays
// bracketed wherever it appears.
func originURL(scheme, host string, port int) string {
	return scheme + "://" + net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(port))
}

// addrHost extracts the host of a "host:port" address. A value with no port keeps whatever
// it is, so a bare hostname or a bracketed IPv6 literal still answers.
func addrHost(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	if addr == "" || addr == "0.0.0.0" || addr == "::" {
		return addr
	}
	return strings.Trim(addr, "[]")
}

// addrPort returns the port of a "host:port" address, or the fallback when it has none.
func addrPort(addr string, fallback int) int {
	if _, port, err := net.SplitHostPort(addr); err == nil {
		if value, convErr := strconv.Atoi(port); convErr == nil {
			return value
		}
	}
	return fallback
}

// isLoopbackHost reports whether a bind host can only be reached from this host.
func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

// addrIP reads the IP out of the two address shapes net.InterfaceAddrs returns.
func addrIP(addr net.Addr) net.IP {
	switch value := addr.(type) {
	case *net.IPNet:
		return value.IP
	case *net.IPAddr:
		return value.IP
	default:
		return nil
	}
}
