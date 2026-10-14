package core

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// ParseShareLink parses one proxy share link into (protocol, sing-box outbound fragment, error).
// The fragment has no "tag"; the caller assigns it.
func ParseShareLink(link string) (string, map[string]any, error) {
	link = strings.TrimSpace(link)
	if link == "" {
		return "", nil, fmt.Errorf("empty link")
	}
	switch {
	case strings.HasPrefix(link, "ss://"):
		return parseSS(link)
	case strings.HasPrefix(link, "vmess://"):
		return parseVmess(link)
	case strings.HasPrefix(link, "vless://"):
		return parseVless(link)
	case strings.HasPrefix(link, "trojan://"):
		return parseTrojan(link)
	case strings.HasPrefix(link, "hysteria2://") || strings.HasPrefix(link, "hy2://"):
		return parseHysteria2(link)
	case strings.HasPrefix(link, "tuic://"):
		return parseTuic(link)
	default:
		return "", nil, fmt.Errorf("unsupported scheme")
	}
}

func b64decode(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "#"); i >= 0 {
		s = s[:i]
	}
	// tolerate missing padding and url-safe alphabet
	if m := len(s) % 4; m != 0 {
		s += strings.Repeat("=", 4-m)
	}
	if b, err := base64.URLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.StdEncoding.DecodeString(s)
}

func splitHostPort(hp string) (string, int, error) {
	host, portStr, err := net.SplitHostPort(hp)
	if err != nil {
		return "", 0, err
	}
	p, err := strconv.Atoi(portStr)
	if err != nil {
		return "", 0, err
	}
	return strings.Trim(host, "[]"), p, nil
}

// ---------- shadowsocks ----------

func parseSS(link string) (string, map[string]any, error) {
	rest := strings.TrimPrefix(link, "ss://")
	frag := ""
	if i := strings.Index(rest, "#"); i >= 0 {
		frag = rest[i+1:]
		rest = rest[:i]
	}
	var method, password, host string
	var port int
	if strings.Contains(rest, "@") {
		// ss://base64(method:password)@host:port
		parts := strings.SplitN(rest, "@", 2)
		up, err := b64decode(parts[0])
		if err != nil {
			return "", nil, fmt.Errorf("ss userinfo decode: %w", err)
		}
		upStr := string(up)
		j := strings.LastIndex(upStr, ":")
		if j < 0 {
			return "", nil, fmt.Errorf("ss bad userinfo")
		}
		method, password = upStr[:j], upStr[j+1:]
		host, port, err = splitHostPort(parts[1])
		if err != nil {
			return "", nil, fmt.Errorf("ss hostport: %w", err)
		}
	} else {
		// ss://base64(method:password@host:port)
		raw, err := b64decode(rest)
		if err != nil {
			return "", nil, fmt.Errorf("ss decode: %w", err)
		}
		s := string(raw)
		at := strings.LastIndex(s, "@")
		if at < 0 {
			return "", nil, fmt.Errorf("ss bad format")
		}
		upStr := s[:at]
		j := strings.LastIndex(upStr, ":")
		if j < 0 {
			return "", nil, fmt.Errorf("ss bad userinfo")
		}
		method, password = upStr[:j], upStr[j+1:]
		host, port, err = splitHostPort(s[at+1:])
		if err != nil {
			return "", nil, fmt.Errorf("ss hostport: %w", err)
		}
	}
	_ = frag // display tag is extracted by caller via TagFromLink
	ob := map[string]any{
		"type": "shadowsocks", "server": host, "server_port": port,
		"method": method, "password": password,
	}
	return "shadowsocks", ob, nil
}

// ---------- vmess ----------

type vmessJSON struct {
	Ps   string `json:"ps"`
	Add  string `json:"add"`
	Port any    `json:"port"`
	ID   string `json:"id"`
	Aid  any    `json:"aid"`
	Scy  string `json:"scy"`
	Net  string `json:"net"`
	Type string `json:"type"`
	Host string `json:"host"`
	Path string `json:"path"`
	TLS  string `json:"tls"`
	Sni  string `json:"sni"`
	Alpn string `json:"alpn"`
	Fp   string `json:"fp"`
}

func anyInt(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case string:
		n, _ := strconv.Atoi(t)
		return n
	case int:
		return t
	}
	return 0
}

func parseVmess(link string) (string, map[string]any, error) {
	raw, err := b64decode(strings.TrimPrefix(link, "vmess://"))
	if err != nil {
		return "", nil, fmt.Errorf("vmess decode: %w", err)
	}
	var v vmessJSON
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", nil, fmt.Errorf("vmess json: %w", err)
	}
	if v.Add == "" || v.ID == "" {
		return "", nil, fmt.Errorf("vmess missing host/id")
	}
	sec := v.Scy
	if sec == "" {
		sec = "auto"
	}
	ob := map[string]any{
		"type": "vmess", "server": v.Add, "server_port": anyInt(v.Port),
		"uuid": v.ID, "security": sec, "alter_id": anyInt(v.Aid),
	}
	applyStream(ob, v.Net, v.Type, v.Host, v.Path, v.TLS, v.Sni, v.Alpn, v.Fp, "")
	return "vmess", ob, nil
}

// applyStream attaches tls/transport/multiplex options shared by vmess/vless/trojan-ish links.
func applyStream(ob map[string]any, net_, type_, host, path, tls, sni, alpn, fp, pbk string) {
	if tls == "tls" || tls == "reality" {
		t := map[string]any{"enabled": true}
		if sni != "" {
			t["server_name"] = sni
		} else if h, ok := ob["server"].(string); ok {
			t["server_name"] = h
		}
		if alpn != "" {
			t["alpn"] = strings.Split(alpn, ",")
		}
		if fp != "" {
			t["utls"] = map[string]any{"enabled": true, "fingerprint": fp}
		}
		if tls == "reality" {
			t["reality"] = map[string]any{"enabled": true}
			if pbk != "" {
				t["reality"].(map[string]any)["public_key"] = pbk
			}
		}
		ob["tls"] = t
	}
	switch net_ {
	case "ws":
		tr := map[string]any{"type": "ws"}
		if path != "" {
			tr["path"] = path
		}
		if host != "" {
			tr["headers"] = map[string]any{"Host": host}
		}
		ob["transport"] = tr
	case "grpc":
		tr := map[string]any{"type": "grpc"}
		if path != "" {
			tr["service_name"] = path
		}
		ob["transport"] = tr
	case "httpupgrade":
		tr := map[string]any{"type": "httpupgrade"}
		if path != "" {
			tr["path"] = path
		}
		if host != "" {
			tr["host"] = host
		}
		ob["transport"] = tr
	case "h2":
		tr := map[string]any{"type": "http"}
		if path != "" {
			tr["path"] = path
		}
		if host != "" {
			tr["host"] = []string{host}
		}
		ob["transport"] = tr
	}
	if type_ == "" || type_ == "none" {
		// plain tcp, nothing to add
	}
}

// ---------- vless ----------

func parseVless(link string) (string, map[string]any, error) {
	// manual parse: vless://uuid@host:port?params#tag
	rest := strings.TrimPrefix(link, "vless://")
	query := ""
	if i := strings.Index(rest, "?"); i >= 0 {
		query = rest[i+1:]
		rest = rest[:i]
	}
	if i := strings.Index(query, "#"); i >= 0 {
		query = query[:i]
	}
	if i := strings.Index(rest, "#"); i >= 0 {
		rest = rest[:i]
	}
	at := strings.LastIndex(rest, "@")
	if at < 0 {
		return "", nil, fmt.Errorf("vless bad format")
	}
	uuid := rest[:at]
	host, port, err := splitHostPort(rest[at+1:])
	if err != nil {
		return "", nil, fmt.Errorf("vless hostport: %w", err)
	}
	q, _ := url.ParseQuery(query)
	ob := map[string]any{
		"type": "vless", "server": host, "server_port": port, "uuid": uuid,
	}
	if enc := q.Get("encryption"); enc != "" && enc != "none" {
		ob["encryption"] = enc
	}
	if flow := q.Get("flow"); flow != "" {
		ob["flow"] = flow
	}
	tlsMode := q.Get("security")
	applyStream(ob, q.Get("type"), q.Get("headerType"), q.Get("host"), q.Get("path"),
		tlsMode, q.Get("sni"), q.Get("alpn"), q.Get("fp"), q.Get("pbk"))
	if tlsMode == "reality" {
		if sid := q.Get("sid"); sid != "" {
			if r, ok := ob["tls"].(map[string]any)["reality"].(map[string]any); ok {
				r["short_id"] = sid
			}
		}
	}
	return "vless", ob, nil
}

// ---------- trojan ----------

func parseTrojan(link string) (string, map[string]any, error) {
	rest := strings.TrimPrefix(link, "trojan://")
	if i := strings.Index(rest, "?"); i >= 0 {
		rest = rest[:i]
	}
	if i := strings.Index(rest, "#"); i >= 0 {
		rest = rest[:i]
	}
	at := strings.LastIndex(rest, "@")
	if at < 0 {
		return "", nil, fmt.Errorf("trojan bad format")
	}
	password, _ := url.PathUnescape(rest[:at])
	host, port, err := splitHostPort(rest[at+1:])
	if err != nil {
		return "", nil, fmt.Errorf("trojan hostport: %w", err)
	}
	u, _ := url.Parse(link)
	q := u.Query()
	tls := map[string]any{"enabled": true}
	if sni := q.Get("sni"); sni != "" {
		tls["server_name"] = sni
	} else {
		tls["server_name"] = host
	}
	if q.Get("allowInsecure") == "1" || q.Get("insecure") == "1" {
		tls["insecure"] = true
	}
	if alpn := q.Get("alpn"); alpn != "" {
		tls["alpn"] = strings.Split(alpn, ",")
	}
	ob := map[string]any{
		"type": "trojan", "server": host, "server_port": port,
		"password": password, "tls": tls,
	}
	applyStream(ob, q.Get("type"), "", q.Get("host"), q.Get("path"), "",
		"", "", "", "")
	ob["tls"] = tls // keep trojan-specific tls (sni/insecure/alpn)
	return "trojan", ob, nil
}

// ---------- hysteria2 ----------

func parseHysteria2(link string) (string, map[string]any, error) {
	rest := link
	for _, p := range []string{"hysteria2://", "hy2://"} {
		rest = strings.TrimPrefix(rest, p)
	}
	if i := strings.Index(rest, "?"); i >= 0 {
		rest = rest[:i]
	}
	if i := strings.Index(rest, "#"); i >= 0 {
		rest = rest[:i]
	}
	at := strings.LastIndex(rest, "@")
	if at < 0 {
		return "", nil, fmt.Errorf("hysteria2 bad format")
	}
	password, _ := url.PathUnescape(rest[:at])
	host, port, err := splitHostPort(rest[at+1:])
	if err != nil {
		return "", nil, fmt.Errorf("hysteria2 hostport: %w", err)
	}
	u, _ := url.Parse(link)
	q := u.Query()
	tls := map[string]any{"enabled": true}
	if sni := q.Get("sni"); sni != "" {
		tls["server_name"] = sni
	} else {
		tls["server_name"] = host
	}
	if q.Get("insecure") == "1" {
		tls["insecure"] = true
	}
	ob := map[string]any{
		"type": "hysteria2", "server": host, "server_port": port,
		"password": password, "tls": tls,
	}
	if obfs := q.Get("obfs"); obfs != "" {
		ob["obfs"] = map[string]any{"type": obfs, "password": q.Get("obfs-password")}
	}
	return "hysteria2", ob, nil
}

// ---------- tuic ----------

func parseTuic(link string) (string, map[string]any, error) {
	rest := strings.TrimPrefix(link, "tuic://")
	if i := strings.Index(rest, "?"); i >= 0 {
		rest = rest[:i]
	}
	if i := strings.Index(rest, "#"); i >= 0 {
		rest = rest[:i]
	}
	at := strings.LastIndex(rest, "@")
	if at < 0 {
		return "", nil, fmt.Errorf("tuic bad format")
	}
	up := rest[:at]
	uuid, password := up, ""
	if i := strings.Index(up, ":"); i >= 0 {
		uuid, password = up[:i], up[i+1:]
	}
	uuid, _ = url.PathUnescape(uuid)
	password, _ = url.PathUnescape(password)
	host, port, err := splitHostPort(rest[at+1:])
	if err != nil {
		return "", nil, fmt.Errorf("tuic hostport: %w", err)
	}
	u, _ := url.Parse(link)
	q := u.Query()
	tls := map[string]any{"enabled": true}
	if sni := q.Get("sni"); sni != "" {
		tls["server_name"] = sni
	} else {
		tls["server_name"] = host
	}
	if q.Get("allow_insecure") == "1" || q.Get("insecure") == "1" {
		tls["insecure"] = true
	}
	if alpn := q.Get("alpn"); alpn != "" {
		tls["alpn"] = strings.Split(alpn, ",")
	}
	ob := map[string]any{
		"type": "tuic", "server": host, "server_port": port,
		"uuid": uuid, "password": password, "tls": tls,
		"congestion_control": "bbr",
	}
	return "tuic", ob, nil
}

// TagFromLink extracts the #fragment display name.
func TagFromLink(link string) string {
	if i := strings.LastIndex(link, "#"); i >= 0 && i+1 < len(link) {
		if t, err := url.PathUnescape(link[i+1:]); err == nil && t != "" {
			return t
		}
	}
	return ""
}

// LinkTag returns the display name for a share link: URL #fragment first,
// falling back to vmess JSON "ps".
func LinkTag(link, proto string) string {
	if t := TagFromLink(link); t != "" {
		return t
	}
	if proto == "vmess" {
		raw, err := b64decode(strings.TrimPrefix(strings.TrimSpace(link), "vmess://"))
		if err == nil {
			var v vmessJSON
			if json.Unmarshal(raw, &v) == nil && v.Ps != "" {
				return v.Ps
			}
		}
	}
	return ""
}
