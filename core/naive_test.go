package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const testNaiveLink = "naive+https://testuser:test-pass-123@chuang.kdns.fr:24448#se-naive"

func TestParseNaiveHTTPS(t *testing.T) {
	proto, ob, err := ParseShareLink(testNaiveLink)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if proto != "naive" || ob["type"] != "naive" {
		t.Fatalf("unexpected proto/outbound: %q %v", proto, ob)
	}
	if ob["server"] != "chuang.kdns.fr" || ob["server_port"] != 24448 ||
		ob["username"] != "testuser" || ob["password"] != "test-pass-123" ||
		ob["network"] != "https" {
		t.Fatalf("bad endpoint/credentials: %v", ob)
	}
	if tag := LinkTag(testNaiveLink, proto); tag != "se-naive" {
		t.Fatalf("tag: %q", tag)
	}
}

func TestParseNaiveQUIC(t *testing.T) {
	proto, ob, err := ParseShareLink("naive+quic://u1:p1@host.example:8443#q")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if proto != "naive" || ob["network"] != "quic" || ob["server_port"] != 8443 {
		t.Fatalf("bad quic outbound: %v", ob)
	}
}

// The bare naive:// form has no explicit transport and means https;
// a missing port defaults to 443.
func TestParseNaiveBareAndDefaultPort(t *testing.T) {
	_, ob, err := ParseShareLink("naive://u1:p1@host.example#n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if ob["network"] != "https" || ob["server_port"] != 443 {
		t.Fatalf("bad bare outbound: %v", ob)
	}
	_, ob, err = ParseShareLink("naive+https://u1:p1@host.example")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if ob["server_port"] != 443 {
		t.Fatalf("default port: %v", ob)
	}
	if tag := LinkTag("naive+https://u1:p1@host.example", "naive"); tag != "" {
		t.Fatalf("expected empty tag, got %q", tag)
	}
}

func TestParseNaiveErrors(t *testing.T) {
	for _, link := range []string{
		"naive+https://host.example:443#x",       // no credentials at all
		"naive+https://user@host.example:443#x",  // no password
		"naive+https://:pw@host.example:443#x",   // no username
		"naive+https://u:p@:443#x",               // no host
		"naive+https://u:p@host.example:zz#x",    // bad port
		"naive+https://u:p@host.example:99999#x", // port out of range
		"naive+http://u:p@host.example:443#x",    // http is not a naive transport
		"naive+quic://u:p@#x",                    // no host (quic)
	} {
		if _, _, err := ParseShareLink(link); err == nil {
			t.Fatalf("expected error for %q", link)
		}
	}
}

// BuildNaiveLink must escape URL-significant credential characters
// so the generated link parses back to exactly the same node.
func TestNaiveLinkRoundTrip(t *testing.T) {
	pw := "p@ss:w#rd!x/y?z"
	link := BuildNaiveLink("quic", "us er", pw, "chuang.kdns.fr", 24448, "my node")
	proto, ob, err := ParseShareLink(link)
	if err != nil {
		t.Fatalf("reparse %q: %v", link, err)
	}
	if proto != "naive" || ob["password"] != pw || ob["username"] != "us er" ||
		ob["server"] != "chuang.kdns.fr" || ob["server_port"] != 24448 || ob["network"] != "quic" {
		t.Fatalf("round trip mismatch: %v", ob)
	}
	if tag := LinkTag(link, proto); tag != "my node" {
		t.Fatalf("tag round trip: %q", tag)
	}
}

func naiveTestNode() Node {
	return Node{
		ID: "n-nv1", Tag: "se-naive", Protocol: "naive",
		Outbound: map[string]any{
			"type": "naive", "server": "chuang.kdns.fr",
			"server_port": 24448, "username": "testuser",
			"password": "test-pass-123", "network": "https",
		},
		Source: "manual",
	}
}

// BuildConfig must render a naive node as a socks outbound to a
// probed local port, listed in the selector and urltest groups like
// any other node -- and the naive credentials must never leak into
// the sing-box config.
func TestBuildConfigNaiveBecomesSocks(t *testing.T) {
	dir := t.TempDir()
	d := StoreData{Nodes: []Node{naiveTestNode()}, Cfg: DefaultSettings()}
	if _, err := BuildConfig(d, dir); err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"test-pass-123", "testuser"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("naive credential %q leaked into sing-box config", secret)
		}
	}
	var cfg struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	var socks map[string]any
	for _, ob := range cfg.Outbounds {
		if ob["tag"] == "node:n-nv1" {
			socks = ob
		}
		if ob["tag"] == "proxy" || ob["tag"] == "auto" {
			members, _ := ob["outbounds"].([]any)
			found := false
			for _, m := range members {
				if m == "node:n-nv1" {
					found = true
				}
			}
			if !found {
				t.Fatalf("group %v does not contain the naive node: %v", ob["tag"], ob)
			}
		}
	}
	if socks == nil {
		t.Fatal("node outbound missing from config")
	}
	if socks["type"] != "socks" || socks["server"] != "127.0.0.1" || socks["version"] != "5" {
		t.Fatalf("not a socks outbound: %v", socks)
	}
	if p, _ := socks["server_port"].(float64); p <= 0 || p > 65535 {
		t.Fatalf("bad socks port: %v", socks["server_port"])
	}
}

// A store mixing privproto and naive nodes must give each external
// client its own local port.
func TestBuildConfigExternalClientsGetDistinctPorts(t *testing.T) {
	dir := t.TempDir()
	d := StoreData{Nodes: []Node{privprotoTestNode(), naiveTestNode()}, Cfg: DefaultSettings()}
	if _, err := BuildConfig(d, dir); err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	ports := map[string]float64{}
	for _, ob := range cfg.Outbounds {
		if ob["type"] == "socks" {
			ports[ob["tag"].(string)], _ = ob["server_port"].(float64)
		}
	}
	pp, nv := ports["node:n-pp1"], ports["node:n-nv1"]
	if pp <= 0 || nv <= 0 {
		t.Fatalf("missing socks ports: %v", ports)
	}
	if pp == nv {
		t.Fatalf("privproto and naive share local port %v", pp)
	}
}

// The Manager must derive the same port from the written config
// that BuildConfig put there, and rebuild the exact upstream URL.
func TestNaiveSpecsFromStoreAndConfig(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	store.Data.Nodes = []Node{naiveTestNode()}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildConfig(store.Data, dir); err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}
	specs, err := NewManager(dir).naiveSpecs()
	if err != nil {
		t.Fatalf("naiveSpecs: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("expected 1 spec, got %d", len(specs))
	}
	s := specs[0]
	want := "https://testuser:test-pass-123@chuang.kdns.fr:24448"
	if s.proxy != want || s.port <= 0 {
		t.Fatalf("bad spec: %+v (want proxy %q)", s, want)
	}
}

func TestNaiveClientBinDiscovery(t *testing.T) {
	name := "naive"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binDir := t.TempDir()
	binPath := filepath.Join(binDir, name)
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	got, err := naiveClientBin(t.TempDir())
	if err != nil {
		t.Fatalf("discovery via PATH: %v", err)
	}
	if got != binPath {
		t.Fatalf("expected %q, got %q", binPath, got)
	}

	t.Setenv("PATH", t.TempDir())
	if _, err := naiveClientBin(t.TempDir()); err == nil ||
		!strings.Contains(err.Error(), "naive client binary not found") {
		t.Fatalf("expected a clear not-found error, got: %v", err)
	}
}

// A naive node must survive a store save/load round trip and
// DeleteNode must remove exactly it.
func TestNaiveNodeStoreRoundTripAndDelete(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	other := Node{ID: "n-x", Tag: "x", Protocol: "vless",
		Outbound: map[string]any{"type": "vless"}, Source: "manual"}
	store.Data.Nodes = []Node{naiveTestNode(), other}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened.Data.Nodes) != 2 || !IsNaiveNode(reopened.Data.Nodes[0]) {
		t.Fatalf("round trip broke the naive node: %+v", reopened.Data.Nodes)
	}
	if !DeleteNode(reopened, "n-nv1") {
		t.Fatal("DeleteNode returned false")
	}
	if len(reopened.Data.Nodes) != 1 || reopened.Data.Nodes[0].ID != "n-x" {
		t.Fatalf("delete removed the wrong node: %+v", reopened.Data.Nodes)
	}
	if DeleteNode(reopened, "n-nv1") {
		t.Fatal("second DeleteNode should return false")
	}
}
