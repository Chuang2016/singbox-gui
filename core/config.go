package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// BuildConfig renders a sing-box config.json from store data.
func BuildConfig(d StoreData, workDir string) (string, error) {
	if len(d.Nodes) == 0 {
		return "", fmt.Errorf("no nodes configured")
	}
	nodeTags := make([]string, 0, len(d.Nodes))
	tagOf := map[string]string{}
	for _, n := range d.Nodes {
		t := "node:" + n.ID
		nodeTags = append(nodeTags, t)
		tagOf[n.ID] = t
	}

	// selector default
	defTag := nodeTags[0]
	if d.Cfg.ActiveNodeID != "" {
		if t, ok := tagOf[d.Cfg.ActiveNodeID]; ok {
			defTag = t
		}
	}

	outbounds := []any{}
	for _, n := range d.Nodes {
		ob := map[string]any{}
		for k, v := range n.Outbound {
			ob[k] = v
		}
		ob["tag"] = tagOf[n.ID]
		outbounds = append(outbounds, ob)
	}
	// selector + urltest + direct + block + dns
	outbounds = append(outbounds,
		map[string]any{"type": "selector", "tag": "proxy", "outbounds": append([]string{"auto"}, nodeTags...), "default": defTag},
		map[string]any{"type": "urltest", "tag": "auto", "outbounds": nodeTags,
			"url": "https://www.gstatic.com/generate_204", "interval": "10m", "tolerance": 50},
		map[string]any{"type": "direct", "tag": "direct", "domain_resolver": "google"},
		map[string]any{"type": "block", "tag": "block"},
	)

	rules := []any{
		map[string]any{"protocol": "dns", "action": "hijack-dns"},
		map[string]any{"action": "sniff"},
	}
	ruleSets := []any{}
	dnsRules := []any{}
	switch d.Cfg.Mode {
	case "global-proxy":
		// everything through proxy; keep only dns rule
	case "global-direct":
		rules = append(rules, map[string]any{"outbound": "direct"})
	default: // bypass-cn
		ruleSets = append(ruleSets,
			map[string]any{"type": "remote", "tag": "geosite-cn", "format": "binary",
				"url":         "https://raw.githubusercontent.com/SagerNet/sing-geosite/rule-set/geosite-cn.srs",
				"http_client": "rs-client"},
			map[string]any{"type": "remote", "tag": "geoip-cn", "format": "binary",
				"url":         "https://raw.githubusercontent.com/SagerNet/sing-geoip/rule-set/geoip-cn.srs",
				"http_client": "rs-client"},
		)
		rules = append(rules,
			map[string]any{"rule_set": []string{"geosite-cn"}, "outbound": "direct"},
			map[string]any{"rule_set": []string{"geoip-cn"}, "outbound": "direct"},
		)
		dnsRules = append(dnsRules,
			map[string]any{"action": "route", "server": "local", "rule_set": "geosite-cn"},
		)
	}
	// custom rules (before final)
	for _, r := range d.Cfg.CustomRules {
		rule := map[string]any{"outbound": r.Outbound}
		switch r.Kind {
		case "ip":
			rule["ip_cidr"] = []string{r.Value}
		default:
			rule["domain_suffix"] = []string{r.Value}
		}
		rules = append(rules, rule)
	}
	final := "proxy"
	if d.Cfg.Mode == "global-direct" {
		final = "direct"
	}

	cfg := map[string]any{
		"log": map[string]any{"level": "warn", "timestamp": true},
		"dns": map[string]any{
			"servers": []any{
				map[string]any{"tag": "google", "type": "tls", "server": "8.8.8.8"},
				map[string]any{"tag": "local", "type": "udp", "server": "223.5.5.5", "detour": "direct"},
			},
			"rules": dnsRules,
			"final": "google",
		},
		"inbounds": []any{
			map[string]any{"type": "mixed", "tag": "mixed-in",
				"listen": "127.0.0.1", "listen_port": d.Cfg.MixedPort},
		},
		"outbounds": outbounds,
		"route": map[string]any{
			"rules":                   rules,
			"rule_set":                ruleSets,
			"final":                   final,
			"default_domain_resolver": map[string]any{"server": "google"},
			"default_http_client":     "rs-client",
		},
		"http_clients": []any{
			map[string]any{"tag": "rs-client", "detour": "proxy"},
		},
		"experimental": map[string]any{
			"clash_api": map[string]any{
				"external_controller": fmt.Sprintf("127.0.0.1:%d", d.Cfg.ClashPort),
				"secret":              "",
			},
		},
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	path := filepath.Join(workDir, "config.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		return "", err
	}
	return path, nil
}
