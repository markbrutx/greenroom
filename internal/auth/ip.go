package auth

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

func PeerIP(r *http.Request) netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return a.Unmap()
}

func Trusted(ip netip.Addr, proxies []netip.Prefix) bool {
	for _, p := range proxies {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

func ClientIP(r *http.Request, proxies []netip.Prefix) string {
	peer := PeerIP(r)
	if !peer.IsValid() {
		return r.RemoteAddr
	}
	if !Trusted(peer, proxies) {
		return peer.String()
	}
	hops := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			break
		}
		a = a.Unmap()
		if !Trusted(a, proxies) {
			return a.String()
		}
	}
	return peer.String()
}

func (a *Auth) ClientIP(r *http.Request) string { return ClientIP(r, a.cfg.TrustedProxies) }
