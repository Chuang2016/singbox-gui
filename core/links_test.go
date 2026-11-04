package core

import (
	"testing"
)

const testRealityLink = "vless://1703f214-108f-48ab-8e81-8ae15e924812@trustclover.eu.org:24443?type=tcp&security=reality&pbk=zvFuwtb6TVWn9jIztmEnSQFSU71neR64kbTObLO6ViM&sid=e913e59428cf408d&fp=chrome&sni=www.apple.com&flow=xtls-rprx-vision#ai-vless-reality"

// same node, but exported without fp= (a common panel/sub-converter shape)
const testRealityLinkNoFp = "vless://1703f214-108f-48ab-8e81-8ae15e924812@trustclover.eu.org:24443?type=tcp&security=reality&pbk=zvFuwtb6TVWn9jIztmEnSQFSU71neR64kbTObLO6ViM&sid=e913e59428cf408d&sni=www.apple.com&flow=xtls-rprx-vision#ai-vless-reality"

const testAnytlsLink = "anytls://VmfyQvPHKI@trustclover.eu.org:51416?security=tls&insecure=1&alpn=h2,http/1.1,h3#anytls-51416"

func tlsOf(t *testing.T, ob map[string]any) map[string]any {
	t.Helper()
	tls, ok := ob["tls"].(map[string]any)
	if !ok {
		t.Fatalf("outbound has no tls block: %v", ob)
	}
	return tls
}

func TestParseVlessReality(t *testing.T) {
	proto, ob, err := ParseShareLink(testRealityLink)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if proto != "vless" || ob["flow"] != "xtls-rprx-vision" {
		t.Fatalf("unexpected outbound: %v", ob)
	}
	tls := tlsOf(t, ob)
	r, ok := tls["reality"].(map[string]any)
	if !ok || r["public_key"] == "" || r["short_id"] == "" {
		t.Fatalf("reality block incomplete: %v", tls)
	}
	u, ok := tls["utls"].(map[string]any)
	if !ok || u["fingerprint"] != "chrome" {
		t.Fatalf("utls missing or wrong: %v", tls)
	}
}

// Regression: a REALITY link without fp= used to produce an outbound with
// no uTLS, and sing-box then refused to start the whole config
// ("uTLS is required by reality client").
func TestParseVlessRealityDefaultsUtls(t *testing.T) {
	_, ob, err := ParseShareLink(testRealityLinkNoFp)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	tls := tlsOf(t, ob)
	u, ok := tls["utls"].(map[string]any)
	if !ok || u["enabled"] != true || u["fingerprint"] != "chrome" {
		t.Fatalf("expected default chrome utls, got: %v", tls["utls"])
	}
}

func TestParseAnytls(t *testing.T) {
	proto, ob, err := ParseShareLink(testAnytlsLink)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if proto != "anytls" || ob["type"] != "anytls" {
		t.Fatalf("unexpected proto/outbound: %q %v", proto, ob)
	}
	if ob["password"] != "VmfyQvPHKI" || ob["server_port"] != 51416 {
		t.Fatalf("bad credentials/endpoint: %v", ob)
	}
	tls := tlsOf(t, ob)
	if tls["insecure"] != true {
		t.Fatalf("insecure not carried over: %v", tls)
	}
	if alpn, ok := tls["alpn"].([]string); !ok || len(alpn) != 3 {
		t.Fatalf("alpn not carried over: %v", tls)
	}
}

func TestClashAnytlsAndRealityUtlsDefault(t *testing.T) {
	yamlText := `
proxies:
  - name: at
    type: anytls
    server: trustclover.eu.org
    port: 51416
    password: VmfyQvPHKI
    sni: trustclover.eu.org
    skip-cert-verify: true
  - name: rl
    type: vless
    server: example.com
    port: 443
    uuid: 1703f214-108f-48ab-8e81-8ae15e924812
    tls: true
    servername: www.apple.com
    reality-opts:
      public-key: zvFuwtb6TVWn9jIztmEnSQFSU71neR64kbTObLO6ViM
      short-id: e913e59428cf408d
`
	obs, _, protos, err := parseClashYAML(yamlText)
	if err != nil {
		t.Fatalf("clash parse: %v", err)
	}
	if len(obs) != 2 {
		t.Fatalf("expected 2 proxies converted, got %d (%v)", len(obs), protos)
	}
	if obs[0]["type"] != "anytls" || obs[0]["password"] != "VmfyQvPHKI" {
		t.Fatalf("anytls conversion wrong: %v", obs[0])
	}
	tls := tlsOf(t, obs[1])
	if _, ok := tls["utls"].(map[string]any); !ok {
		t.Fatalf("clash reality outbound lacks default utls: %v", tls)
	}
}
