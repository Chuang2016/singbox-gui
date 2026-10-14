package core

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
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
