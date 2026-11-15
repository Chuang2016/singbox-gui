//go:build windows

package main

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"

	"github.com/Chuang2016/singbox-gui/core"
)

// Node editing, mirroring the web dashboard's Edit card: quick fields
// (name / server / port / SNI / WS Host / WS Path) patched into the
// full outbound JSON on save. The CDN workflow is the point: put a
// preferred CDN IP in Server, keep the domain in SNI and WS Host.
// Saving goes through core.UpdateNode, which validates the rebuilt
// config with sing-box and rolls back a rejected edit.

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

func (a *app) editSelectedNode() {
	n := a.selectedNode()
	if n == nil {
		a.fail("Edit node: select a node first")
		return
	}
	node := *n
	ob := node.Outbound
	raw, _ := json.MarshalIndent(ob, "", "  ")

	var dlg *walk.Dialog
	var nameEd, serverEd, portEd, sniEd, hostEd, pathEd *walk.LineEdit
	var jsonEd *walk.TextEdit
	var accept *walk.PushButton

	def := Dialog{
		AssignTo:      &dlg,
		Title:         "Edit node - " + node.Tag,
		DefaultButton: &accept,
		MinSize:       Size{Width: 560, Height: 520},
		Layout:        VBox{Margins: Margins{Left: 10, Top: 10, Right: 10, Bottom: 10}, Spacing: 6},
		Children: []Widget{
			Label{Text: "Name:"},
			LineEdit{AssignTo: &nameEd, Text: node.Tag},
			Composite{
				Layout: Grid{Columns: 2, MarginsZero: true, Spacing: 6},
				Children: []Widget{
					Label{Text: "Server (for CDN nodes: a preferred CDN IP):"},
					Label{Text: "Port:"},
					LineEdit{AssignTo: &serverEd, Text: obGet(ob, "server")},
					LineEdit{AssignTo: &portEd, Text: obGet(ob, "server_port")},
					Label{Text: "SNI (tls.server_name - your domain for CDN nodes):"},
					Label{Text: "WS Host (transport Host header - your domain):"},
					LineEdit{AssignTo: &sniEd, Text: obGet(ob, "tls", "server_name")},
					LineEdit{AssignTo: &hostEd, Text: wsHostOf(ob)},
					Label{Text: "WS Path:"},
					Label{Text: ""},
					LineEdit{AssignTo: &pathEd, Text: obGet(ob, "transport", "path")},
					Label{Text: ""},
				},
			},
			Label{Text: "Full outbound JSON - the quick fields above are patched into it on save:"},
			TextEdit{AssignTo: &jsonEd, Text: string(raw), VScroll: true, MinSize: Size{Height: 180}},
			Composite{
				Layout: HBox{MarginsZero: true, Spacing: 6},
				Children: []Widget{
					HSpacer{},
					PushButton{AssignTo: &accept, Text: "Save node"},
					PushButton{Text: "Cancel", OnClicked: func() { dlg.Cancel() }},
				},
			},
		},
	}
	if err := def.Create(a.mw); err != nil {
		a.fail("Edit node: %v", err)
		return
	}
	accept.Clicked().Attach(func() {
		var newOb map[string]any
		if err := json.Unmarshal([]byte(jsonEd.Text()), &newOb); err != nil {
			walk.MsgBox(dlg, "Edit node", "JSON error: "+err.Error(), walk.MsgBoxIconError|walk.MsgBoxOK)
			return
		}
		obSet(newOb, strings.TrimSpace(serverEd.Text()), "server")
		if pv := strings.TrimSpace(portEd.Text()); pv != "" {
			if p, err := strconv.Atoi(pv); err == nil {
				newOb["server_port"] = float64(p)
			}
		}
		obSet(newOb, strings.TrimSpace(sniEd.Text()), "tls", "server_name")
		// only touch Host when the outbound has a transport or the field is filled
		if strings.TrimSpace(hostEd.Text()) != "" || wsHostOf(newOb) != "" {
			obSet(newOb, strings.TrimSpace(hostEd.Text()), "transport", "headers", "Host")
		}
		if strings.TrimSpace(pathEd.Text()) != "" || obGet(newOb, "transport", "path") != "" {
			obSet(newOb, strings.TrimSpace(pathEd.Text()), "transport", "path")
		}
		if err := core.UpdateNode(a.store, a.mgr, a.workDir, node.ID, nameEd.Text(), newOb); err != nil {
			walk.MsgBox(dlg, "Edit node", "Save failed: "+err.Error(), walk.MsgBoxIconError|walk.MsgBoxOK)
			return
		}
		dlg.Accept()
	})
	if dlg.Run() != walk.DlgCmdOK {
		return
	}
	a.lastNodeCount = len(a.store.Data.Nodes)
	a.model.PublishRowsReset()
	a.syncTableSelection()
	a.logf("Node saved: %s", nameEd.Text())
	a.notice = ""
	a.refreshStatus()
	a.refreshLogs()
	if a.mgr.Running() && walk.MsgBox(a.mw, "Node saved",
		"Restart sing-box to apply the edit?",
		walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) == walk.DlgCmdYes {
		_ = a.mgr.Stop()
		if err := a.startProxy(); err != nil {
			a.fail("Restart after edit failed: %v", err)
		} else {
			a.logf("Restarted to apply the node edit")
			a.refreshStatus()
			a.refreshLogs()
		}
	}
}

// wsHostOf reads transport.headers.Host (or host) from an outbound.
func wsHostOf(ob map[string]any) string {
	if h := obGet(ob, "transport", "headers", "Host"); h != "" {
		return h
	}
	return obGet(ob, "transport", "headers", "host")
}
