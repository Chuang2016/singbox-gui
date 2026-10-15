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
		map[string]any{"type": "direct", "tag": "direct", "domain_resolver": "local"},
		map[string]any{"type": "block", "tag": "block"},
	)

	rules := []any{
		map[string]any{"protocol": "dns", "action": "hijack-dns"},
		map[string]any{"action": "sniff"},
	}
	ruleSets := []any{}
	// user rules first: highest priority, override all built-in packs
	if n, err := WriteUserList(workDir, "user-domains", "domain", d.Cfg.UserDomains); err == nil && n > 0 {
		ob := d.Cfg.UserDomainOb
		if ob == "" {
			ob = "proxy"
		}
		ruleSets = append(ruleSets,
			map[string]any{"type": "local", "tag": "user-domains", "format": "source",
				"path": filepath.Join(RuleSetDir(workDir), "user-domains.json")})
		rules = append(rules,
			map[string]any{"rule_set": []string{"user-domains"}, "outbound": ob})
	}
	if n, err := WriteUserList(workDir, "user-ips", "ip", d.Cfg.UserIPs); err == nil && n > 0 {
		ob := d.Cfg.UserIPOb
		if ob == "" {
			ob = "proxy"
		}
		ruleSets = append(ruleSets,
			map[string]any{"type": "local", "tag": "user-ips", "format": "source",
				"path": filepath.Join(RuleSetDir(workDir), "user-ips.json")})
		rules = append(rules,
			map[string]any{"rule_set": []string{"user-ips"}, "outbound": ob})
	}
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
	// ad/tracker block in every mode (after user rules)
	if d.Cfg.BlockAds {
		ruleSets = append(ruleSets,
			map[string]any{"type": "local", "tag": "ads", "format": "binary",
				"path": filepath.Join(RuleSetDir(workDir), "geosite-category-ads-all.srs")})
		rules = append(rules,
			map[string]any{"rule_set": []string{"ads"}, "outbound": "block"})
	}
	switch d.Cfg.Mode {
	case "global-proxy":
		// everything through proxy; keep only dns rule
	case "global-direct":
		rules = append(rules, map[string]any{"outbound": "direct"})
	default: // bypass-cn
		// local rule-sets (downloaded by sbgui via CN-friendly mirrors, no proxy needed)
		for _, tag := range []string{"geosite-cn", "geoip-cn"} {
			ruleSets = append(ruleSets,
				map[string]any{"type": "local", "tag": tag, "format": "binary",
					"path": filepath.Join(RuleSetDir(workDir), tag+".srs")})
		}
		// GFW list first: blocked domains go proxy even if DNS returns a CN IP
		if d.Cfg.GFWList {
			ruleSets = append(ruleSets,
				map[string]any{"type": "local", "tag": "gfwlist", "format": "source",
					"path": filepath.Join(RuleSetDir(workDir), "gfwlist.json")})
			rules = append(rules,
				map[string]any{"rule_set": []string{"gfwlist"}, "outbound": "proxy"})
		}
		rules = append(rules,
			map[string]any{"rule_set": []string{"geosite-cn"}, "outbound": "direct"},
			map[string]any{"rule_set": []string{"geoip-cn"}, "outbound": "direct"},
		)
	}
	final := "proxy"
	if d.Cfg.Mode == "global-direct" {
		final = "direct"
	}

	// DNS split: CN domains -> local resolver (direct, fast);
	// foreign domains -> clean resolver via proxy (no pollution).
	// Node hostnames always resolve via local (no circular proxy dependency).
	dnsServers := []any{
		map[string]any{"tag": "local", "type": "udp", "server": "223.5.5.5", "detour": "direct"},
	}
	dnsRules := []any{}
	dnsFinal := "local"
	if d.Cfg.Mode != "global-direct" {
		dnsServers = append(dnsServers,
			map[string]any{"tag": "remote", "type": "tls", "server": "8.8.8.8", "detour": "proxy"})
		dnsFinal = "remote"
	}
	if d.Cfg.Mode == "bypass-cn" {
		dnsRules = append(dnsRules,
			map[string]any{"action": "route", "server": "local", "rule_set": "geosite-cn"})
	}

	inbounds := []any{
		map[string]any{"type": "mixed", "tag": "mixed-in",
			"listen": "127.0.0.1", "listen_port": d.Cfg.MixedPort},
	}
	if d.Cfg.GatewayMode {
		inbounds = append(inbounds, map[string]any{"type": "tproxy", "tag": "tproxy-in",
			"listen": "0.0.0.0", "listen_port": GatewayTproxyPort, "network": []string{"tcp", "udp"}})
	}

	cfg := map[string]any{
		"log": map[string]any{"level": "warn", "timestamp": true},
		"dns": map[string]any{
			"servers": dnsServers,
			"rules":   dnsRules,
			"final":   dnsFinal,
		},
		"inbounds": inbounds,
		"outbounds": outbounds,
		"route": map[string]any{
			"rules":                   rules,
			"rule_set":                ruleSets,
			"final":                   final,
			"default_domain_resolver": map[string]any{"server": "local"},
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
