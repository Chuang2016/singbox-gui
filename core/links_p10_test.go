package core

import "testing"

// The http2:// form (emitted by S-UI for naive inbounds) carries
// the whole authority base64-encoded and is equivalent to a
// naive+https:// link.
func TestParseHTTP2Naive(t *testing.T) {
	// base64 authority decodes to user1:pass-123@naive.example:24448
	link := "http2://dXNlcjE6cGFzcy0xMjNAbmFpdmUuZXhhbXBsZToyNDQ0OA==?padding=1&alpn=h2,http/1.1,h3&insecure=1&tfo=0#ai-naive"
	proto, ob, err := ParseShareLink(link)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if proto != "naive" || ob["type"] != "naive" {
		t.Fatalf("unexpected proto/outbound: %q %v", proto, ob)
	}
	if ob["server"] != "naive.example" || ob["server_port"] != 24448 ||
		ob["username"] != "user1" || ob["password"] != "pass-123" ||
		ob["network"] != "https" {
		t.Fatalf("bad endpoint/credentials: %v", ob)
	}
	if tag := LinkTag(link, proto); tag != "ai-naive" {
		t.Fatalf("tag: %q", tag)
	}
}

// A WebSocket node's TLS ALPN list must not keep "h2": a CDN
// edge negotiates it and the HTTP/1.1 WS upgrade then dies with
// EOF. Other entries (and ALPN on non-WS transports) are kept.
func TestParseVlessWSStripsH2ALPN(t *testing.T) {
	link := "vless://11111111-2222-3333-4444-555555555555@203.0.113.10:2053?type=ws&path=%2Fray2&host=cdn.example&security=tls&allowInsecure=1&sni=cdn.example&alpn=h3,h2,http/1.1#ws-cdn"
	_, ob, err := ParseShareLink(link)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	tls, ok := ob["tls"].(map[string]any)
	if !ok {
		t.Fatalf("no tls in outbound: %v", ob)
	}
	alpn, _ := tls["alpn"].([]string)
	for _, a := range alpn {
		if a == "h2" {
			t.Fatalf("h2 kept in ws alpn: %v", alpn)
		}
	}
	if len(alpn) != 2 {
		t.Fatalf("expected [h3 http/1.1], got %v", alpn)
	}
	tr, _ := ob["transport"].(map[string]any)
	if tr["type"] != "ws" || tr["path"] != "/ray2" {
		t.Fatalf("bad ws transport: %v", tr)
	}
}

// A server port of 0 marks a server-side placeholder (the S-UI
// shadowtls detour companion), not a dialable node: the link is
// rejected at import time like any other unparsable line.
func TestParseShareLinkPortZeroRejected(t *testing.T) {
	link := "ss://YWVzLTI1Ni1nY206cGFzc3dvcmQ=@ss.example:0#ai-ss-in"
	if _, _, err := ParseShareLink(link); err == nil {
		t.Fatalf("expected port-0 link to be rejected")
	}
}
