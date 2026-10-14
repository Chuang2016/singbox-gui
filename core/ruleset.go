package core

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Rule-set sources: jsdelivr first (reachable from CN), then upstream.
var ruleSetMirrors = map[string][]string{
	"geosite-cn": {
		"https://cdn.jsdelivr.net/gh/SagerNet/sing-geosite@rule-set/rule-set/geosite-cn.srs",
		"https://raw.githubusercontent.com/SagerNet/sing-geosite/rule-set/geosite-cn.srs",
	},
	"geoip-cn": {
		"https://cdn.jsdelivr.net/gh/SagerNet/sing-geoip@rule-set/rule-set/geoip-cn.srs",
		"https://raw.githubusercontent.com/SagerNet/sing-geoip/rule-set/geoip-cn.srs",
	},
}

const ruleSetMaxAge = 7 * 24 * time.Hour

// RuleSetDir is where downloaded .srs files live (under work dir).
func RuleSetDir(workDir string) string { return filepath.Join(workDir, "ruleset") }

// EnsureRuleSets downloads geosite-cn / geoip-cn .srs files into workDir/ruleset
// when missing or older than 7 days. Uses CN-friendly mirrors; no proxy needed.
func EnsureRuleSets(workDir string) error {
	dir := RuleSetDir(workDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	for tag, urls := range ruleSetMirrors {
		path := filepath.Join(dir, tag+".srs")
		if st, err := os.Stat(path); err == nil && st.Size() > 1024 && time.Since(st.ModTime()) < ruleSetMaxAge {
			continue // fresh cache
		}
		var lastErr error
		for _, u := range urls {
			if err := downloadFile(u, path+".tmp"); err != nil {
				lastErr = err
				continue
			}
			if err := os.Rename(path+".tmp", path); err != nil {
				lastErr = err
				continue
			}
			lastErr = nil
			break
		}
		if lastErr != nil {
			return fmt.Errorf("download ruleset %s failed (tried %d mirrors): %w", tag, len(urls), lastErr)
		}
	}
	return nil
}

func downloadFile(url, dest string) error {
	client := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "sbgui/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("http %d", resp.StatusCode)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, io.LimitReader(resp.Body, 64<<20))
	f.Close()
	if err != nil {
		os.Remove(dest)
		return err
	}
	if n < 1024 {
		os.Remove(dest)
		return fmt.Errorf("suspiciously small file (%d bytes)", n)
	}
	return nil
}

// Extra downloadable packs.
var extraMirrors = map[string][]string{
	"geosite-category-ads-all": {
		"https://cdn.jsdelivr.net/gh/SagerNet/sing-geosite@rule-set/rule-set/geosite-category-ads-all.srs",
		"https://raw.githubusercontent.com/SagerNet/sing-geosite/rule-set/geosite-category-ads-all.srs",
	},
}

var gfwlistMirrors = []string{
	"https://cdn.jsdelivr.net/gh/gfwlist/gfwlist@master/gfwlist.txt",
	"https://raw.githubusercontent.com/gfwlist/gfwlist/master/gfwlist.txt",
}

// EnsureExtraRuleSets downloads optional .srs packs (e.g. ads) into workDir/ruleset.
func EnsureExtraRuleSets(workDir string, tags []string) error {
	dir := RuleSetDir(workDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	for _, tag := range tags {
		urls, ok := extraMirrors[tag]
		if !ok {
			return fmt.Errorf("unknown rule-set pack: %s", tag)
		}
		path := filepath.Join(dir, tag+".srs")
		if st, err := os.Stat(path); err == nil && st.Size() > 1024 && time.Since(st.ModTime()) < ruleSetMaxAge {
			continue
		}
		var lastErr error
		for _, u := range urls {
			if err := downloadFile(u, path+".tmp"); err != nil {
				lastErr = err
				continue
			}
			if err := os.Rename(path+".tmp", path); err != nil {
				lastErr = err
				continue
			}
			lastErr = nil
			break
		}
		if lastErr != nil {
			return fmt.Errorf("download ruleset %s failed: %w", tag, lastErr)
		}
	}
	return nil
}

// EnsureGFWList downloads the gfwlist, converts it to a sing-box source
// rule-set (domain_suffix list) at workDir/ruleset/gfwlist.json.
// @@ exceptions are removed from the blocked set (they fall through to
// normal routing, i.e. "don't proxy").
func EnsureGFWList(workDir string) error {
	dir := RuleSetDir(workDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	path := filepath.Join(dir, "gfwlist.json")
	if st, err := os.Stat(path); err == nil && st.Size() > 1024 && time.Since(st.ModTime()) < ruleSetMaxAge {
		return nil // fresh cache
	}
	var body []byte
	var lastErr error
	for _, u := range gfwlistMirrors {
		b, err := fetchBytes(u)
		if err != nil {
			lastErr = err
			continue
		}
		body, lastErr = b, nil
		break
	}
	if lastErr != nil {
		return fmt.Errorf("download gfwlist failed: %w", lastErr)
	}
	domains, _ := parseGFWList(body)
	if len(domains) < 100 {
		return fmt.Errorf("gfwlist parse: only %d domains, refusing", len(domains))
	}
	doc := map[string]any{
		"version": 3,
		"rules": []any{
			map[string]any{"domain_suffix": sortedKeys(domains)},
		},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func fetchBytes(url string) ([]byte, error) {
	client := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "sbgui/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

// parseGFWList decodes base64 gfwlist and extracts ||domain / |http(s)://domain
// entries plus @@ exceptions. Returns (blocked, exceptions).
func parseGFWList(body []byte) (map[string]bool, map[string]bool) {
	blocked := map[string]bool{}
	exceptions := map[string]bool{}
	// strip whitespace, tolerate missing/non-strict padding
	s := strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, strings.TrimSpace(string(body)))
	decoded, err := base64.RawStdEncoding.DecodeString(s)
	if err != nil {
		// lenient fallback: gfwlist's final quantum has non-zero trailing
		// bits which Go's strict decoder rejects; drop it (<=3 bytes lost)
		if n := len(s) % 4; n != 0 {
			s = s[:len(s)-n]
		} else if len(s) >= 4 {
			s = s[:len(s)-4]
		}
		if decoded, err = base64.RawStdEncoding.DecodeString(s); err != nil {
			return blocked, exceptions
		}
	}
	for _, line := range strings.Split(string(decoded), "\n") {
		l := strings.TrimSpace(line)
		if l == "" || strings.HasPrefix(l, "!") || strings.HasPrefix(l, "[") {
			continue
		}
		target := blocked
		if strings.HasPrefix(l, "@@") {
			target = exceptions
			l = strings.TrimPrefix(l, "@@")
		}
		var domain string
		switch {
		case strings.HasPrefix(l, "||"):
			domain = strings.TrimPrefix(l, "||")
		case strings.HasPrefix(l, "|http"):
			rest := strings.TrimPrefix(strings.TrimPrefix(l, "|http://"), "|https://")
			if i := strings.IndexAny(rest, "/?#"); i >= 0 {
				rest = rest[:i]
			}
			domain = rest
		default:
			continue // regex, cosmetic, or unsupported
		}
		// strip port / trailing dot / leading wildcard
		domain = strings.ToLower(domain)
		if i := strings.Index(domain, ":"); i >= 0 {
			domain = domain[:i]
		}
		domain = strings.Trim(domain, ".")
		domain = strings.TrimPrefix(domain, "*.")
		if domain == "" || strings.ContainsAny(domain, "/*?") {
			continue
		}
		target[domain] = true
	}
	// exceptions win over blocked
	for d := range exceptions {
		delete(blocked, d)
	}
	return blocked, exceptions
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
