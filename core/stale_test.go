package core

import (
	"path/filepath"
	"testing"
)

func TestMatchSingboxArgv(t *testing.T) {
	cfg := filepath.Join("/sb", "config.json")
	bin := filepath.Join("/sb", "sing-box")
	cases := []struct {
		name string
		argv []string
		want bool
	}{
		{"bundled exact", []string{bin, "run", "-c", cfg}, true},
		{"path name", []string{"sing-box", "run", "-c", cfg}, true},
		{"exe name", []string{"C:/sb/sing-box.exe", "run", "-c", cfg}, true},
		{"trailing args ok", []string{bin, "run", "-c", cfg, "-l", "info"}, true},
		{"wrong config", []string{bin, "run", "-c", filepath.Join("/other", "config.json")}, false},
		{"check is not run", []string{bin, "check", "-c", cfg}, false},
		{"editor with config open", []string{"vim", cfg}, false},
		{"wrong binary", []string{"/usr/bin/other", "run", "-c", cfg}, false},
		{"too short", []string{bin, "run"}, false},
		{"no args", nil, false},
	}
	for _, tc := range cases {
		if got := matchSingboxArgv(tc.argv, cfg, bin); got != tc.want {
			t.Errorf("%s: matchSingboxArgv(%v) = %v, want %v", tc.name, tc.argv, got, tc.want)
		}
	}
}

func TestMatchSingboxCmdline(t *testing.T) {
	cfg := "/sb/config.json"
	bin := "/sb/sing-box"
	cases := []struct {
		name string
		cmd  string
		bin  string
		want bool
	}{
		{"bundled exact", "/sb/sing-box run -c /sb/config.json", bin, true},
		{"path name", "sing-box run -c /sb/config.json", bin, true},
		{"trailing args ok", "/sb/sing-box run -c /sb/config.json -l info", bin, true},
		{"editor with config open", "vim /sb/config.json", bin, false},
		{"config path is a prefix", "/sb/sing-box run -c /sb/config.json.bak", bin, false},
		{"wrong config", "/sb/sing-box run -c /other/config.json", bin, false},
		{"wrong binary", "/usr/bin/other run -c /sb/config.json", bin, false},
		{"mention in a shell line", "echo sing-box run -c /sb/config.json", bin, false},
		{"spaces in bundled path", "/My Apps/sb/sing-box run -c /sb/config.json", "/My Apps/sb/sing-box", true},
		{"empty", "", bin, false},
	}
	for _, tc := range cases {
		if got := matchSingboxCmdline(tc.cmd, cfg, tc.bin); got != tc.want {
			t.Errorf("%s: matchSingboxCmdline(%q) = %v, want %v", tc.name, tc.cmd, got, tc.want)
		}
	}
}
