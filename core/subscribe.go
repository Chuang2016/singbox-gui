package core

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// FetchSubscription downloads url and returns parsed outbounds (fragments without tag)
// plus display tags. Supports: base64/v2rayN line list, sing-box JSON, clash YAML.
func FetchSubscription(rawURL string) ([]map[string]any, []string, []string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, nil, nil, fmt.Errorf("empty url")
	}
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, nil, nil, err
	}
	req.Header.Set("User-Agent", "singbox-gui/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, nil, nil, err
	}
	text := strings.TrimSpace(string(body))
	if text == "" {
		return nil, nil, nil, fmt.Errorf("empty subscription body")
	}
	// try base64 whole-body decode (v2rayN style)
	if decoded, err := tryB64(text); err == nil {
		text = strings.TrimSpace(string(decoded))
	}
	switch {
	case strings.HasPrefix(text, "{"):
		return parseSingboxJSON(text)
	case strings.Contains(text, "proxies:"):
		return parseClashYAML(text)
	default:
		return parseLinkList(text)
	}
}

func tryB64(s string) ([]byte, error) {
	clean := strings.Map(func(r rune) rune {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '+' || r == '/' || r == '=' || r == '-' || r == '_' {
			return r
		}
		return -1
	}, s)
	if len(clean) < 16 || strings.Contains(clean, "://") {
		return nil, fmt.Errorf("not b64")
	}
	if m := len(clean) % 4; m != 0 {
		clean += strings.Repeat("=", 4-m)
	}
	if b, err := base64.URLEncoding.DecodeString(clean); err == nil {
		return b, nil
	}
	return base64.StdEncoding.DecodeString(clean)
}

func parseLinkList(text string) ([]map[string]any, []string, []string, error) {
	var obs []map[string]any
	var tags []string
	var protos []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		proto, ob, err := ParseShareLink(line)
		if err != nil {
			continue // skip bad lines
		}
		tag := LinkTag(line, proto)
		if tag == "" {
			tag = fmt.Sprintf("%s-%d", proto, len(obs)+1)
		}
		obs = append(obs, ob)
		tags = append(tags, tag)
		protos = append(protos, proto)
	}
	if len(obs) == 0 {
		return nil, nil, nil, fmt.Errorf("no valid nodes in subscription")
	}
	return obs, tags, protos, nil
}

func parseSingboxJSON(text string) ([]map[string]any, []string, []string, error) {
	var doc struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		return nil, nil, nil, fmt.Errorf("sing-box json: %w", err)
	}
	var obs []map[string]any
	var tags []string
	var protos []string
	skip := map[string]bool{"direct": true, "block": true, "dns": true, "selector": true, "urltest": true}
	for _, o := range doc.Outbounds {
		t, _ := o["type"].(string)
		if skip[t] || t == "" {
			continue
		}
		tag, _ := o["tag"].(string)
		delete(o, "tag")
		if tag == "" {
			tag = fmt.Sprintf("%s-%d", t, len(obs)+1)
		}
		obs = append(obs, o)
		tags = append(tags, tag)
		protos = append(protos, t)
	}
	if len(obs) == 0 {
		return nil, nil, nil, fmt.Errorf("no usable outbounds in json")
	}
	return obs, tags, protos, nil
}

// ---------- clash yaml ----------

type clashProxy struct {
	Name           string         `yaml:"name"`
	Type           string         `yaml:"type"`
	Server         string         `yaml:"server"`
	Port           any            `yaml:"port"`
	Password       string         `yaml:"password"`
	UUID           string         `yaml:"uuid"`
	Cipher         string         `yaml:"cipher"`
	UDP            bool           `yaml:"udp"`
	TLS            bool           `yaml:"tls"`
	SNI            string         `yaml:"sni"`
	ServerName     string         `yaml:"servername"`
	SkipCertVerify bool           `yaml:"skip-cert-verify"`
	Network        string         `yaml:"network"`
	WsOpts         map[string]any `yaml:"ws-opts"`
	GrpcOpts       map[string]any `yaml:"grpc-opts"`
	Flow           string         `yaml:"flow"`
	Alpn           []string       `yaml:"alpn"`
	Fingerprint    string         `yaml:"fingerprint"`
	ClientFingerprint string      `yaml:"client-fingerprint"`
	RealityOpts    map[string]any `yaml:"reality-opts"`
}

func parseClashYAML(text string) ([]map[string]any, []string, []string, error) {
	var doc struct {
		Proxies []clashProxy `yaml:"proxies"`
	}
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		return nil, nil, nil, fmt.Errorf("clash yaml: %w", err)
	}
	var obs []map[string]any
	var tags []string
	var protos []string
	for _, p := range doc.Proxies {
		ob, proto := clashToOutbound(p)
		if ob == nil {
			continue
		}
		tag := p.Name
		if tag == "" {
			tag = fmt.Sprintf("%s-%d", proto, len(obs)+1)
		}
		obs = append(obs, ob)
		tags = append(tags, tag)
		protos = append(protos, proto)
	}
	if len(obs) == 0 {
		return nil, nil, nil, fmt.Errorf("no usable proxies in clash yaml")
	}
	return obs, tags, protos, nil
}

func clashTLS(p clashProxy, server string) map[string]any {
	if !p.TLS {
		return nil
	}
	t := map[string]any{"enabled": true}
	sni := p.SNI
	if sni == "" {
		sni = p.ServerName
	}
	if sni == "" {
		sni = server
	}
	t["server_name"] = sni
	if p.SkipCertVerify {
		t["insecure"] = true
	}
	if len(p.Alpn) > 0 {
		t["alpn"] = p.Alpn
	}
	fp := p.Fingerprint
	if fp == "" {
		fp = p.ClientFingerprint
	}
	if fp != "" {
		t["utls"] = map[string]any{"enabled": true, "fingerprint": fp}
	}
	if p.RealityOpts != nil {
		r := map[string]any{"enabled": true}
		if pk, ok := p.RealityOpts["public-key"].(string); ok {
			r["public_key"] = pk
		}
		if sid, ok := p.RealityOpts["short-id"].(string); ok {
			r["short_id"] = sid
		}
		t["reality"] = r
	}
	return t
}

func clashTransport(p clashProxy) map[string]any {
	switch p.Network {
	case "ws":
		tr := map[string]any{"type": "ws"}
		if path, ok := p.WsOpts["path"].(string); ok && path != "" {
			tr["path"] = path
		}
		var host string
		if hs, ok := p.WsOpts["headers"].(map[string]any); ok {
			host, _ = hs["Host"].(string)
		}
		if host != "" {
			tr["headers"] = map[string]any{"Host": host}
		}
		return tr
	case "grpc":
		tr := map[string]any{"type": "grpc"}
		if sn, ok := p.GrpcOpts["grpc-service-name"].(string); ok && sn != "" {
			tr["service_name"] = sn
		}
		return tr
	}
	return nil
}

func clashToOutbound(p clashProxy) (map[string]any, string) {
	if p.Server == "" {
		return nil, ""
	}
	port := anyInt(p.Port)
	base := map[string]any{"server": p.Server, "server_port": port}
	tls := clashTLS(p, p.Server)
	tr := clashTransport(p)
	switch strings.ToLower(p.Type) {
	case "ss", "shadowsocks":
		base["type"] = "shadowsocks"
		base["method"] = p.Cipher
		base["password"] = p.Password
		return base, "shadowsocks"
	case "vmess":
		base["type"] = "vmess"
		base["uuid"] = p.UUID
		base["security"] = "auto"
		base["alter_id"] = 0
		if tls != nil {
			base["tls"] = tls
		}
		if tr != nil {
			base["transport"] = tr
		}
		return base, "vmess"
	case "vless":
		base["type"] = "vless"
		base["uuid"] = p.UUID
		if p.Flow != "" {
			base["flow"] = p.Flow
		}
		if tls != nil {
			base["tls"] = tls
		}
		if tr != nil {
			base["transport"] = tr
		}
		return base, "vless"
	case "trojan":
		base["type"] = "trojan"
		base["password"] = p.Password
		if tls == nil {
			tls = map[string]any{"enabled": true, "server_name": p.Server}
		}
		base["tls"] = tls
		if tr != nil {
			base["transport"] = tr
		}
		return base, "trojan"
	case "hysteria2", "hy2":
		base["type"] = "hysteria2"
		base["password"] = p.Password
		if tls == nil {
			tls = map[string]any{"enabled": true, "server_name": p.Server}
		}
		base["tls"] = tls
		return base, "hysteria2"
	case "tuic":
		base["type"] = "tuic"
		base["uuid"] = p.UUID
		base["password"] = p.Password
		if tls == nil {
			tls = map[string]any{"enabled": true, "server_name": p.Server}
		}
		base["tls"] = tls
		base["congestion_control"] = "bbr"
		return base, "tuic"
	}
	return nil, ""
}
