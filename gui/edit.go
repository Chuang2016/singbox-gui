package main

import (
	"encoding/json"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/Chuang2016/singbox-gui/core"
)

// Node editing, mirroring the web dashboard's Edit card and the Walk
// front-end: quick fields (name / server / port / SNI / WS Host / WS
// Path) patched into the full outbound JSON on save. The CDN
// workflow is the point: put a preferred CDN IP in Server, keep the
// domain in SNI and WS Host. Saving goes through core.UpdateNode,
// which validates the rebuilt config with sing-box and rolls back a
// rejected edit.

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

func (g *guiState) editSelectedNode() {
	n := g.targetNode()
	if n == nil {
		g.fail("Edit node: select a node first")
		return
	}
	node := *n
	ob := node.Outbound
	raw, _ := json.MarshalIndent(ob, "", "  ")

	nameEd := widget.NewEntry()
	nameEd.SetText(node.Tag)
	serverEd := widget.NewEntry()
	serverEd.SetText(obGet(ob, "server"))
	portEd := widget.NewEntry()
	portEd.SetText(obGet(ob, "server_port"))
	sniEd := widget.NewEntry()
	sniEd.SetText(obGet(ob, "tls", "server_name"))
	hostEd := widget.NewEntry()
	hostEd.SetText(wsHostOf(ob))
	pathEd := widget.NewEntry()
	pathEd.SetText(obGet(ob, "transport", "path"))
	jsonEd := widget.NewMultiLineEntry()
	jsonEd.SetText(string(raw))
	jsonEd.SetMinRowsVisible(12)

	form := container.NewVBox(
		widget.NewForm(
			widget.NewFormItem("Name", nameEd),
			widget.NewFormItem("Server (CDN: preferred IP)", serverEd),
			widget.NewFormItem("Port", portEd),
			widget.NewFormItem("SNI (tls.server_name)", sniEd),
			widget.NewFormItem("WS Host (transport Host)", hostEd),
			widget.NewFormItem("WS Path", pathEd),
		),
		widget.NewLabel("Full outbound JSON — the quick fields above are patched into it on save:"),
		jsonEd,
	)

	var d dialog.Dialog
	saveBtn := widget.NewButton("Save node", func() {
		var newOb map[string]any
		if err := json.Unmarshal([]byte(jsonEd.Text), &newOb); err != nil {
			dialog.ShowError(err, g.win)
			return
		}
		obSet(newOb, strings.TrimSpace(serverEd.Text), "server")
		if pv := strings.TrimSpace(portEd.Text); pv != "" {
			if p, err := strconv.Atoi(pv); err == nil {
				newOb["server_port"] = float64(p)
			}
		}
		obSet(newOb, strings.TrimSpace(sniEd.Text), "tls", "server_name")
		// only touch Host when the outbound has a transport or the field is filled
		if strings.TrimSpace(hostEd.Text) != "" || wsHostOf(newOb) != "" {
			obSet(newOb, strings.TrimSpace(hostEd.Text), "transport", "headers", "Host")
		}
		if strings.TrimSpace(pathEd.Text) != "" || obGet(newOb, "transport", "path") != "" {
			obSet(newOb, strings.TrimSpace(pathEd.Text), "transport", "path")
		}
		if err := core.UpdateNode(g.store, g.mgr, g.workDir, node.ID, nameEd.Text, newOb); err != nil {
			dialog.ShowError(err, g.win)
			return
		}
		d.Hide()
		if g.nodeTable != nil {
			g.nodeTable.Refresh()
		}
		g.logf("Node saved: %s", nameEd.Text)
		g.notice = ""
		g.refreshStatus()
		if g.mgr.Running() {
			dialog.ShowConfirm("Node saved", "Restart sing-box to apply the edit?", func(ok bool) {
				if ok {
					g.restartProxy()
				}
			}, g.win)
		}
	})
	saveBtn.Importance = widget.HighImportance
	cancelBtn := widget.NewButton("Cancel", func() { d.Hide() })
	buttons := container.NewBorder(nil, nil, nil,
		container.NewHBox(cancelBtn, saveBtn), nil)
	content := container.NewBorder(nil, buttons, nil, nil,
		container.NewVScroll(form))
	d = dialog.NewCustomWithoutButtons("Edit node — "+node.Tag, content, g.win)
	d.Resize(fyne.NewSize(620, 560))
	d.Show()
}
