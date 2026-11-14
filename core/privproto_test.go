package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const testPrivprotoLink = "privproto://test-pw-123@chuang.kdns.fr:443#se-privproto"

func TestParsePrivproto(t *testing.T) {
	proto, ob, err := ParseShareLink(testPrivprotoLink)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if proto != "privproto" || ob["type"] != "privproto" {
		t.Fatalf("unexpected proto/outbound: %q %v", proto, ob)
	}
	if ob["server"] != "chuang.kdns.fr" || ob["server_port"] != 443 || ob["password"] != "test-pw-123" {
		t.Fatalf("bad endpoint/credentials: %v", ob)
	}
	if tag := LinkTag(testPrivprotoLink, proto); tag != "se-privproto" {
		t.Fatalf("tag: %q", tag)
	}
}

// The raw (unescaped) password form must parse too: real-world
// passwords contain characters like '!' that need no escaping.
func TestParsePrivprotoRawPassword(t *testing.T) {
	_, ob, err := ParseShareLink("privproto://Pv9!kQ2w-x@host.example:8443#n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if ob["password"] != "Pv9!kQ2w-x" || ob["server_port"] != 8443 {
		t.Fatalf("bad outbound: %v", ob)
	}
}

func TestParsePrivprotoErrors(t *testing.T) {
	for _, link := range []string{
		"privproto://@host.example:443#x",  // no password
		"privproto://pw@:443#x",            // no host
		"privproto://pw@host.example#x",    // no port
		"privproto://pw@host.example:zz#x", // bad port
		"privproto://host.example:443#x",   // no @ at all
	} {
		if _, _, err := ParseShareLink(link); err == nil {
			t.Fatalf("expected error for %q", link)
		}
	}
}

// BuildPrivprotoLink must escape URL-significant password characters
// so the generated link parses back to exactly the same node.
func TestPrivprotoLinkRoundTrip(t *testing.T) {
	pw := "p@ss:w#rd!x/y?z"
	link := BuildPrivprotoLink(pw, "chuang.kdns.fr", 443, "my node")
	for _, esc := range []string{"%40", "%23"} {
		if !strings.Contains(link, esc) {
			t.Fatalf("link %q lacks escaping %s", link, esc)
		}
	}
	proto, ob, err := ParseShareLink(link)
	if err != nil {
		t.Fatalf("reparse %q: %v", link, err)
	}
	if proto != "privproto" || ob["password"] != pw || ob["server"] != "chuang.kdns.fr" || ob["server_port"] != 443 {
		t.Fatalf("round trip mismatch: %v", ob)
	}
	if tag := LinkTag(link, proto); tag != "my node" {
		t.Fatalf("tag round trip: %q", tag)
	}
}

func privprotoTestNode() Node {
	return Node{
		ID: "n-pp1", Tag: "se-privproto", Protocol: "privproto",
		Outbound: map[string]any{
			"type": "privproto", "server": "chuang.kdns.fr",
			"server_port": 443, "password": "test-pw-123",
		},
		Source: "manual",
	}
}

// BuildConfig must render a privproto node as a socks outbound to a
// probed local port -- and the privproto password must never leak
// into the sing-box config.
func TestBuildConfigPrivprotoBecomesSocks(t *testing.T) {
	dir := t.TempDir()
	d := StoreData{Nodes: []Node{privprotoTestNode()}, Cfg: DefaultSettings()}
	if _, err := BuildConfig(d, dir); err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "test-pw-123") {
		t.Fatal("privproto password leaked into sing-box config")
	}
	var cfg struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	var socks map[string]any
	for _, ob := range cfg.Outbounds {
		if ob["tag"] == "node:n-pp1" {
			socks = ob
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

// The Manager must derive the same port from the written config
// that BuildConfig put there.
func TestPrivprotoSpecsFromStoreAndConfig(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	store.Data.Nodes = []Node{privprotoTestNode()}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildConfig(store.Data, dir); err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}
	specs, err := NewManager(dir).privprotoSpecs()
	if err != nil {
		t.Fatalf("privprotoSpecs: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("expected 1 spec, got %d", len(specs))
	}
	s := specs[0]
	if s.server != "chuang.kdns.fr:443" || s.password != "test-pw-123" || s.port <= 0 {
		t.Fatalf("bad spec: %+v", s)
	}
}

func TestPrivprotoClientBinDiscovery(t *testing.T) {
	name := "privproto-client"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binDir := t.TempDir()
	binPath := filepath.Join(binDir, name)
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	got, err := privprotoClientBin(t.TempDir())
	if err != nil {
		t.Fatalf("discovery via PATH: %v", err)
	}
	if got != binPath {
		t.Fatalf("expected %q, got %q", binPath, got)
	}

	t.Setenv("PATH", t.TempDir())
	if _, err := privprotoClientBin(t.TempDir()); err == nil ||
		!strings.Contains(err.Error(), "privproto client binary not found") {
		t.Fatalf("expected a clear not-found error, got: %v", err)
	}
}
