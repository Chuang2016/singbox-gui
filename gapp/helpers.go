package main

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/Chuang2016/singbox-gui/core"
)

// humanBytes formats a byte count the way the dashboard does.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// humanRate formats bytes/second for the status bar.
func humanRate(bps float64) string {
	if bps < 1024 {
		return fmt.Sprintf("%d B/s", int64(bps))
	}
	return humanBytes(int64(bps)) + "/s"
}

// --- value/label option sets, shared by the Rules page (mirrors gui/) ---

type valueLabel struct{ value, label string }

var (
	modeOptions = []valueLabel{
		{"bypass-cn", "Bypass mainland CN (CN direct, rest via proxy)"},
		{"global-proxy", "Global proxy"},
		{"global-direct", "Global direct"},
	}
	outboundOptions = []valueLabel{
		{"proxy", "Proxy"},
		{"direct", "Direct"},
		{"block", "Block"},
	}
	kindOptions = []valueLabel{
		{"domain", "Domain suffix"},
		{"ip", "IP range"},
	}
)

func indexOfOption(opts []valueLabel, value string) int {
	for i, o := range opts {
		if o.value == value {
			return i
		}
	}
	return 0
}

func labelFor(opts []valueLabel, value string) string {
	return opts[indexOfOption(opts, value)].label
}

// validateRule checks one custom rule the way sing-box will consume
// it (same rules as gui/rules.go).
func validateRule(kind, value string) error {
	if value == "" {
		return fmt.Errorf("rule value is empty")
	}
	if strings.ContainsAny(value, " \t\r\n") {
		return fmt.Errorf("rule value must not contain spaces: %q", value)
	}
	if kind == "ip" {
		if _, _, err := net.ParseCIDR(value); err == nil {
			return nil
		}
		if net.ParseIP(value) != nil {
			return nil
		}
		return fmt.Errorf("%q is not an IP address or CIDR range", value)
	}
	if strings.Contains(value, "://") || strings.Contains(value, "/") {
		return fmt.Errorf("%q does not look like a domain suffix", value)
	}
	return nil
}

// validateIPList checks the "my IPs" bulk list line by line.
func validateIPList(text string) error {
	for _, line := range strings.Split(text, "\n") {
		l := strings.TrimSpace(line)
		if l == "" || strings.HasPrefix(l, "#") || strings.HasPrefix(l, "//") {
			continue
		}
		if _, _, err := net.ParseCIDR(l); err == nil {
			continue
		}
		if net.ParseIP(l) != nil {
			continue
		}
		return fmt.Errorf("invalid IP/CIDR in my-IPs list: %q", l)
	}
	return nil
}

// groupText renders a node's source: "Manual" or its subscription's
// name.
func (a *App) groupText(n core.Node) string {
	if strings.HasPrefix(n.Source, "sub:") {
		id := strings.TrimPrefix(n.Source, "sub:")
		for _, s := range a.store.Data.Subs {
			if s.ID == id {
				return s.Name
			}
		}
		return "Subscription"
	}
	return "Manual"
}

// serverText renders the outbound's server:port (the port may be a
// float64, int or int64 depending on where the outbound came from).
func serverText(n core.Node) string {
	srv, _ := n.Outbound["server"].(string)
	if srv == "" {
		return "-"
	}
	switch p := n.Outbound["server_port"].(type) {
	case float64:
		return srv + ":" + strconv.Itoa(int(p))
	case int:
		return srv + ":" + strconv.Itoa(p)
	case int64:
		return srv + ":" + strconv.FormatInt(p, 10)
	}
	return srv
}

// --- outbound JSON quick-field access (mirrors gui/edit.go) ---

func obGet(ob map[string]any, path ...string) string {
	var cur any = ob
	for _, k := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = m[k]
	}
	switch v := cur.(type) {
	case string:
		return v
	case float64:
		return strconv.Itoa(int(v))
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case json.Number:
		return v.String()
	default:
		return ""
	}
}

func obSet(ob map[string]any, val string, path ...string) {
	cur := ob
	for _, k := range path[:len(path)-1] {
		next, ok := cur[k].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[k] = next
		}
		cur = next
	}
	last := path[len(path)-1]
	if val == "" {
		delete(cur, last)
	} else {
		cur[last] = val
	}
}

// wsHostOf reads transport.headers.Host (or host) from an outbound.
func wsHostOf(ob map[string]any) string {
	if h := obGet(ob, "transport", "headers", "Host"); h != "" {
		return h
	}
	return obGet(ob, "transport", "headers", "host")
}
