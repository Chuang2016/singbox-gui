package core

// Node is one proxy server. Outbound holds a sing-box outbound JSON fragment
// (without the tag, which is assigned at config build time).
type Node struct {
	ID       string         `json:"id"`
	Tag      string         `json:"tag"`
	Protocol string         `json:"protocol"`
	Outbound map[string]any `json:"outbound"`
	Source   string         `json:"source"` // "manual" or "sub:<subID>"
}

// Subscription is a remote node source.
type Subscription struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	UpdatedAt string `json:"updated_at"`
	Count     int    `json:"count"`
}

// CustomRule is a user rule: match domain-suffix / ip-cidr, send to outbound tag.
type CustomRule struct {
	Kind     string `json:"kind"`     // "domain" | "ip"
	Value    string `json:"value"`    // e.g. "example.com" or "1.2.3.0/24"
	Outbound string `json:"outbound"` // "proxy" | "direct" | "block" | node tag
}

// Settings holds global options.
// GatewayTproxyPort is the listen port of the sing-box tproxy inbound used
// by gateway (transparent proxy) mode. Defined here, not in the Linux-only
// gateway file: config.go references it on every platform.
const GatewayTproxyPort = 2082

type Settings struct {
	Mode         string       `json:"mode"` // "bypass-cn" | "global-proxy" | "global-direct"
	CustomRules  []CustomRule `json:"custom_rules"`
	MixedPort    int          `json:"mixed_port"`
	ClashPort    int          `json:"clash_port"`
	ActiveNodeID string       `json:"active_node_id"` // node selected in "proxy" selector
	AutoUpdateHr int          `json:"auto_update_hr"` // subscription refresh interval, 0 = off
	BlockAds     bool         `json:"block_ads"`      // block ad/tracker domains
	GFWList      bool         `json:"gfwlist"`        // extra gfwlist domain pack -> proxy
	AutoStart    bool         `json:"auto_start"`     // start sing-box automatically on launch
	GatewayMode  bool         `json:"gateway_mode"`   // LAN transparent proxy (Linux + root)
	// user-editable bulk lists (one entry per line), compiled to local rule-sets
	UserDomains  string       `json:"user_domains"`
	UserDomainOb string       `json:"user_domain_ob"` // outbound for user domains
	UserIPs      string       `json:"user_ips"`
	UserIPOb     string       `json:"user_ip_ob"` // outbound for user IPs
}

// StoreData is the persisted file shape.
type StoreData struct {
	Nodes []Node         `json:"nodes"`
	Subs  []Subscription `json:"subs"`
	Cfg   Settings       `json:"settings"`
}

func DefaultSettings() Settings {
	return Settings{
		Mode:         "bypass-cn",
		CustomRules:  []CustomRule{},
		MixedPort:    2080,
		ClashPort:    9090,
		ActiveNodeID: "",
		AutoUpdateHr: 24,
		BlockAds:     true,
		GFWList:      true,
		UserDomainOb: "proxy",
		UserIPOb:     "proxy",
	}
}
