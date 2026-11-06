package portal

import (
	"fmt"
	"net"
	"net/http"
	"strings"
)

// TrustedProxies is the allowlist of immediate TCP peers that may tell GoXIO
// the real client address. A peer outside this list cannot change the
// rate-limit key with X-Forwarded-For or CF-Connecting-IP.
type TrustedProxies struct {
	nets []*net.IPNet
}

// ParseTrustedProxies parses a comma-separated list of IPs and CIDRs.
func ParseTrustedProxies(raw string) (*TrustedProxies, error) {
	t := &TrustedProxies{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if !strings.Contains(part, "/") {
			ip := net.ParseIP(part)
			if ip == nil {
				return nil, fmt.Errorf("trusted proxy %q is not an IP or CIDR", part)
			}
			bits := 128
			if v4 := ip.To4(); v4 != nil {
				ip = v4
				bits = 32
			}
			part = ip.String() + "/" + fmt.Sprintf("%d", bits)
		}
		_, n, err := net.ParseCIDR(part)
		if err != nil {
			return nil, fmt.Errorf("trusted proxy %q: %w", part, err)
		}
		t.nets = append(t.nets, n)
	}
	return t, nil
}

func (t *TrustedProxies) Len() int {
	if t == nil {
		return 0
	}
	return len(t.nets)
}

func (t *TrustedProxies) Allows(ip net.IP) bool {
	if t == nil || ip == nil {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	for _, n := range t.nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func peerIP(r *http.Request) net.IP {
	if r == nil {
		return nil
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return net.ParseIP(strings.TrimSpace(host))
}

// ClientIP is the address used for login and signup limits.
// Forwarded headers count only when the TCP peer is on the allowlist.
// The chosen address is the rightmost forwarded hop that is not itself a
// trusted proxy, which is the client the proxy actually saw. Entries to the
// left of that hop are ignored, so a caller cannot mint a fresh limit by
// putting a documentation address first.
func ClientIP(r *http.Request, trust *TrustedProxies) string {
	peer := peerIP(r)
	if peer == nil || !trust.Allows(peer) {
		if peer == nil {
			return ""
		}
		return peer.String()
	}
	if cf := net.ParseIP(strings.TrimSpace(r.Header.Get("CF-Connecting-IP"))); cf != nil && !trust.Allows(cf) {
		return cf.String()
	}
	if ip := clientFromForwarded(r.Header.Get("X-Forwarded-For"), trust); ip != nil {
		return ip.String()
	}
	return peer.String()
}

func clientFromForwarded(xff string, trust *TrustedProxies) net.IP {
	parts := strings.Split(xff, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		ip := net.ParseIP(strings.TrimSpace(parts[i]))
		if ip == nil {
			continue
		}
		if trust.Allows(ip) {
			continue
		}
		return ip
	}
	return nil
}

// ForwardedHost is X-Forwarded-Host only when the peer is a trusted proxy.
// Otherwise it is the request Host. An untrusted client cannot aim the
// same-origin check at a host it invented.
func ForwardedHost(r *http.Request, trust *TrustedProxies) string {
	if r == nil {
		return ""
	}
	if trust.Allows(peerIP(r)) {
		if h := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); h != "" {
			return strings.TrimSpace(strings.Split(h, ",")[0])
		}
	}
	return r.Host
}
