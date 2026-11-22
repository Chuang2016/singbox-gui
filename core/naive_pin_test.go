package core

import (
	"context"
	"errors"
	"net"
	"testing"
)

// naivePinIPv4 must pin a domain to the first A record returned by
// DNS, and must silently skip pinning (return "") for IP literals,
// lookup failures and AAAA-only names, so those nodes launch
// exactly as they did before pinning existed.

func stubNaiveLookup(t *testing.T, ips []net.IP, err error) {
	t.Helper()
	old := naiveLookupIP
	naiveLookupIP = func(ctx context.Context, host string) ([]net.IP, error) {
		return ips, err
	}
	t.Cleanup(func() { naiveLookupIP = old })
}

func TestNaivePinIPv4Domain(t *testing.T) {
	stubNaiveLookup(t, []net.IP{net.ParseIP("203.0.113.7")}, nil)
	if got := naivePinIPv4("node.example"); got != "MAP node.example 203.0.113.7" {
		t.Fatalf("got %q", got)
	}
}

func TestNaivePinIPv4FirstA(t *testing.T) {
	// An AAAA answer ahead of the A record must be skipped, and
	// the first A record wins.
	stubNaiveLookup(t, []net.IP{
		net.ParseIP("2001:db8::1"),
		net.ParseIP("198.51.100.9"),
		net.ParseIP("198.51.100.10"),
	}, nil)
	if got := naivePinIPv4("node.example"); got != "MAP node.example 198.51.100.9" {
		t.Fatalf("got %q", got)
	}
}

func TestNaivePinIPv4Literals(t *testing.T) {
	// IP literals never consult DNS: stub a lookup that would
	// produce a rule if it were called.
	stubNaiveLookup(t, []net.IP{net.ParseIP("203.0.113.7")}, nil)
	for _, host := range []string{"51.241.150.127", "2001:db8::5", ""} {
		if got := naivePinIPv4(host); got != "" {
			t.Fatalf("host %q: got %q, want no rule", host, got)
		}
	}
}

func TestNaivePinIPv4LookupFails(t *testing.T) {
	stubNaiveLookup(t, nil, errors.New("no such host"))
	if got := naivePinIPv4("missing.example"); got != "" {
		t.Fatalf("got %q, want no rule", got)
	}
}

func TestNaivePinIPv4AAAAOnly(t *testing.T) {
	stubNaiveLookup(t, []net.IP{net.ParseIP("2001:db8::99")}, nil)
	if got := naivePinIPv4("v6only.example"); got != "" {
		t.Fatalf("got %q, want no rule", got)
	}
}
