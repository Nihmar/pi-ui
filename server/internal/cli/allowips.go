package cli

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/Nihmar/pi-ui/server/internal/audit"
)

// ipAllowList refuses a request whose peer is not on the list.
//
// It is a front door, not an authorization: a deployment that publishes the server to a
// network it does not trust uses it to keep the surface from being reachable at all
// (PLAN.md §4.7), and the device token remains what decides what a connection may do. It
// reads the peer address net/http resolved, never a forwarded header: behind a proxy that
// is the proxy's address, which is exactly right — the proxy is the peer.
type ipAllowList struct {
	nets []*net.IPNet
	ips  []net.IP
}

// newIPAllowList parses the flag values: an address (`10.0.0.4`, `::1`) or a CIDR block
// (`192.168.0.0/16`). An empty list allows everything, which is the default.
func newIPAllowList(values []string) (*ipAllowList, error) {
	list := &ipAllowList{}
	for _, value := range values {
		entry := strings.TrimSpace(value)
		if entry == "" {
			continue
		}
		if _, block, err := net.ParseCIDR(entry); err == nil {
			list.nets = append(list.nets, block)
			continue
		}
		ip := net.ParseIP(entry)
		if ip == nil {
			return nil, fmt.Errorf("--allow-ips %q is neither an address nor a CIDR block", entry)
		}
		list.ips = append(list.ips, ip)
	}
	return list, nil
}

// allows reports whether one peer address may connect.
func (l *ipAllowList) allows(remoteAddr string) bool {
	if l == nil || (len(l.nets) == 0 && len(l.ips) == 0) {
		return true
	}
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil {
		// An address net/http cannot parse is not something to guess about.
		return false
	}
	for _, candidate := range l.ips {
		if candidate.Equal(ip) {
			return true
		}
	}
	for _, block := range l.nets {
		if block.Contains(ip) {
			return true
		}
	}
	return false
}

// middleware wraps one handler, answering 403 to a peer outside the list. The refusal is
// the taxonomy's forbidden_scope: the request was understood and refused by policy.
//
// A refusal is recorded: it is the one denial that happens before a credential exists, so
// without this entry the trail would show a gap exactly where somebody was probing the
// server from the wrong network.
func (l *ipAllowList) middleware(next http.Handler, recorder audit.Recorder) http.Handler {
	if l == nil || (len(l.nets) == 0 && len(l.ips) == 0) {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !l.allows(r.RemoteAddr) {
			if recorder != nil {
				recorder.Record(audit.Event{
					Action:     audit.ActionAuthDenied,
					Outcome:    audit.OutcomeDenied,
					RemoteAddr: peerHost(r.RemoteAddr),
					Target:     r.URL.Path,
					Details:    map[string]any{"reason": "allow_ips", "method": r.Method},
				})
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"code":"forbidden_scope","message":"this peer address is not allowed"}}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// peerHost strips the port from a peer address, for the trail: an IPv6 address is a host
// full of colons, so it is read with the same SplitHostPort the allow list uses and left
// alone when it is an address without a port.
func peerHost(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	return strings.Trim(host, "[]")
}

// errNoPeers is what the flag parser reports for a list that parsed to nothing usable.
var errNoPeers = errors.New("no allowed peer addresses")
