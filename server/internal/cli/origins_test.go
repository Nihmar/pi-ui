package cli

import (
	"net"
	"reflect"
	"testing"
)

// ipnet builds the *net.IPNet shape net.InterfaceAddrs returns, so the origin detection is
// tested without touching the machine's real interfaces.
func ipnet(t *testing.T, raw string) net.Addr {
	t.Helper()
	ip := net.ParseIP(raw)
	if ip == nil {
		t.Fatalf("bad test address %q", raw)
	}
	if ip4 := ip.To4(); ip4 != nil {
		return &net.IPNet{IP: ip4, Mask: net.CIDRMask(24, 32)}
	}
	return &net.IPNet{IP: ip, Mask: net.CIDRMask(64, 128)}
}

func TestOriginsFromFiltersOrdersAndDedupes(t *testing.T) {
	addrs := []net.Addr{
		ipnet(t, "127.0.0.1"),                                 // loopback
		ipnet(t, "169.254.10.5"),                              // link-local
		&net.TCPAddr{IP: net.ParseIP("192.168.9.9"), Port: 1}, // not an interface shape
		ipnet(t, "fe80::1"),                                   // IPv6
		ipnet(t, "2001:db8::5"),                               // global IPv6: not dialed here
		ipnet(t, "100.101.102.103"),                           // Tailscale/CGNAT
		ipnet(t, "172.17.0.1"),                                // docker's default bridge
		ipnet(t, "10.0.0.5"),                                  // LAN
		ipnet(t, "192.168.1.20"),                              // LAN
		ipnet(t, "192.168.1.20"),                              // duplicate
		ipnet(t, "8.8.8.8"),                                   // public
	}

	got := originsFrom(addrs, "http", 8787)
	want := []string{
		"http://10.0.0.5:8787",
		"http://192.168.1.20:8787",
		"http://172.17.0.1:8787",
		"http://100.101.102.103:8787",
		"http://8.8.8.8:8787",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("originsFrom = %v, want %v", got, want)
	}

	if got := originsFrom(nil, "http", 8787); len(got) != 0 {
		t.Fatalf("originsFrom(nil) = %v, want none", got)
	}
}

func TestAdvertiseOrigins(t *testing.T) {
	addrs := []net.Addr{ipnet(t, "192.168.1.20"), ipnet(t, "10.0.0.5")}
	detected := []string{"http://192.168.1.20:8787", "http://10.0.0.5:8787"}

	tests := []struct {
		name         string
		bind         string
		port         int
		scheme       string
		want         []string
		wantLoopback bool
	}{
		{
			name: "every interface detects the candidates",
			bind: "0.0.0.0:8787", port: 8787, scheme: "http",
			want: detected,
		},
		{
			name: "an empty host means every interface",
			bind: ":8787", port: 8787, scheme: "http",
			want: detected,
		},
		{
			name: "loopback is this host only",
			bind: "127.0.0.1:9100", port: 9100, scheme: "http",
			want: []string{"http://127.0.0.1:9100"}, wantLoopback: true,
		},
		{
			name: "localhost counts as loopback",
			bind: "localhost:8787", port: 8787, scheme: "http",
			want: []string{"http://localhost:8787"}, wantLoopback: true,
		},
		{
			name: "a bound address pins one origin",
			bind: "192.168.1.20:8787", port: 8787, scheme: "http",
			want: []string{"http://192.168.1.20:8787"},
		},
		{
			name: "a name is one origin",
			bind: "pi-ui.local:8787", port: 8787, scheme: "http",
			want: []string{"http://pi-ui.local:8787"},
		},
		{
			name: "the scheme follows TLS",
			bind: "0.0.0.0:443", port: 443, scheme: "https",
			want: []string{"https://192.168.1.20:443", "https://10.0.0.5:443"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origins, loopback := advertiseOrigins(tt.bind, tt.port, tt.scheme, addrs)
			if !reflect.DeepEqual(origins, tt.want) {
				t.Fatalf("origins = %v, want %v", origins, tt.want)
			}
			if loopback != tt.wantLoopback {
				t.Fatalf("loopback = %v, want %v", loopback, tt.wantLoopback)
			}
		})
	}
}

func TestPairOrigins(t *testing.T) {
	addrs := []net.Addr{ipnet(t, "192.168.1.20")}

	// An explicit origin is exactly what the operator asked for.
	got := pairOrigins("http://pi-ui.local:8787", "", "http", addrs)
	if want := []string{"http://pi-ui.local:8787"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("pairOrigins(--url) = %v, want %v", got, want)
	}

	// No --url and no --addr: the LAN candidates on the default port, the phone case.
	got = pairOrigins("", "", "http", addrs)
	if want := []string{"http://192.168.1.20:8787"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("pairOrigins(detected) = %v, want %v", got, want)
	}

	// An explicit --addr describes where the server binds, port included.
	got = pairOrigins("", "0.0.0.0:9000", "http", addrs)
	if want := []string{"http://192.168.1.20:9000"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("pairOrigins(--addr) = %v, want %v", got, want)
	}
}

func TestAddrHostAndPort(t *testing.T) {
	tests := []struct {
		addr     string
		wantHost string
		wantPort int
	}{
		{addr: "127.0.0.1:8787", wantHost: "127.0.0.1", wantPort: 8787},
		{addr: ":8787", wantHost: "", wantPort: 8787},
		{addr: "[::1]:8787", wantHost: "::1", wantPort: 8787},
		{addr: "::1", wantHost: "::1", wantPort: defaultListenPort},
		{addr: "pi-ui.local:9000", wantHost: "pi-ui.local", wantPort: 9000},
		{addr: "", wantHost: "", wantPort: defaultListenPort},
		{addr: "junk", wantHost: "junk", wantPort: defaultListenPort},
	}
	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			if got := addrHost(tt.addr); got != tt.wantHost {
				t.Errorf("addrHost(%q) = %q, want %q", tt.addr, got, tt.wantHost)
			}
			if got := addrPort(tt.addr, defaultListenPort); got != tt.wantPort {
				t.Errorf("addrPort(%q) = %d, want %d", tt.addr, got, tt.wantPort)
			}
		})
	}
}
