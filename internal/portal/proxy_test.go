package portal

import (
	"net/http"
	"testing"
)

func TestForwardedForDoesNotBypassUntrustedPeer(t *testing.T) {
	trust, err := ParseTrustedProxies("10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	req := httptestRequest("203.0.113.8:443")
	req.Header.Set("X-Forwarded-For", "198.51.100.9")
	req.Header.Set("CF-Connecting-IP", "198.51.100.10")
	if got := ClientIP(req, trust); got != "203.0.113.8" {
		t.Fatalf("untrusted peer client ip = %s", got)
	}
	if got := ClientIP(req, nil); got != "203.0.113.8" {
		t.Fatalf("nil trust client ip = %s", got)
	}
	if got := ForwardedHost(req, trust); got != "example.com" {
		t.Fatalf("untrusted forwarded host = %s", got)
	}
}

func TestTrustedProxyUsesAppendedClientNotSpoofedPrefix(t *testing.T) {
	trust, err := ParseTrustedProxies("10.0.0.8/32, 10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	req := httptestRequest("10.0.0.8:443")
	req.Header.Set("X-Forwarded-For", "198.51.100.50, 203.0.113.77")
	if got := ClientIP(req, trust); got != "203.0.113.77" {
		t.Fatalf("rightmost untrusted hop = %s", got)
	}
	req.Header.Set("X-Forwarded-For", "203.0.113.1, 203.0.113.77")
	if got := ClientIP(req, trust); got != "203.0.113.77" {
		t.Fatalf("changing the left address changed the key: %s", got)
	}
	req.Header.Set("CF-Connecting-IP", "192.0.2.15")
	if got := ClientIP(req, trust); got != "192.0.2.15" {
		t.Fatalf("cf connecting ip = %s", got)
	}
	req.Host = "internal"
	req.Header.Set("X-Forwarded-Host", "goxio.example")
	if got := ForwardedHost(req, trust); got != "goxio.example" {
		t.Fatalf("forwarded host = %s", got)
	}
}

func TestDistinctClientsBehindOneProxyDoNotShareABucket(t *testing.T) {
	trust, err := ParseTrustedProxies("10.1.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	a := httptestRequest("10.1.0.4:443")
	a.Header.Set("CF-Connecting-IP", "192.0.2.1")
	b := httptestRequest("10.1.0.4:443")
	b.Header.Set("CF-Connecting-IP", "192.0.2.2")
	if ClientIP(a, trust) == ClientIP(b, trust) {
		t.Fatal("two clients behind one proxy collapsed to one address")
	}
}

func httptestRequest(remote string) *http.Request {
	req, err := http.NewRequest(http.MethodPost, "http://example.com/api/portal/login", nil)
	if err != nil {
		panic(err)
	}
	req.RemoteAddr = remote
	req.Host = "example.com"
	return req
}
