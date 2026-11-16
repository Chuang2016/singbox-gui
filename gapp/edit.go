package main

import (
	"encoding/json"
	"strconv"
	"strings"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/Chuang2016/singbox-gui/core"
)

// Node editing modal, mirroring gui/edit.go: quick fields (name /
// server / port / SNI / WS Host / WS Path) patched into the full
// outbound JSON on save. Saving goes through core.UpdateNode, which
// validates the rebuilt config with sing-box and rolls back a
// rejected edit.

type editNodeModal struct {
	nodeID string
	tag    string

	nameEd   widget.Editor
	serverEd widget.Editor
	portEd   widget.Editor
	sniEd    widget.Editor
	hostEd   widget.Editor
	pathEd   widget.Editor
	jsonEd   widget.Editor

	saveBtn   widget.Clickable
	cancelBtn widget.Clickable
}

func (a *App) editSelectedNode() {
	n := a.nodes.targetNode(a)
	if n == nil {
		a.fail("Edit node: select a node first")
		return
	}
	node := *n
	raw, _ := json.MarshalIndent(node.Outbound, "", "  ")
	m := &editNodeModal{nodeID: node.ID, tag: node.Tag}
	m.nameEd.SingleLine = true
	m.nameEd.SetText(node.Tag)
	m.serverEd.SingleLine = true
	m.serverEd.SetText(obGet(node.Outbound, "server"))
	m.portEd.SingleLine = true
	m.portEd.SetText(obGet(node.Outbound, "server_port"))
	m.sniEd.SingleLine = true
	m.sniEd.SetText(obGet(node.Outbound, "tls", "server_name"))
	m.hostEd.SingleLine = true
	m.hostEd.SetText(wsHostOf(node.Outbound))
	m.pathEd.SingleLine = true
	m.pathEd.SetText(obGet(node.Outbound, "transport", "path"))
	m.jsonEd.SetText(string(raw))
	a.modal = m
}

func (m *editNodeModal) layoutModal(gtx layout.Context, a *App) layout.Dimensions {
	if m.cancelBtn.Clicked(gtx) {
		a.modal = nil
		return layout.Dimensions{}
	}
	if m.saveBtn.Clicked(gtx) {
		m.save(a)
		if a.modal != m {
			return layout.Dimensions{}
		}
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(modalTitle(a.th, "Edit node — "+m.tag)),
		formRow(a.th, "Name", &m.nameEd, ""),
		formRow(a.th, "Server (CDN: preferred IP)", &m.serverEd, ""),
		formRow(a.th, "Port", &m.portEd, ""),
		formRow(a.th, "SNI (tls.server_name)", &m.sniEd, ""),
		formRow(a.th, "WS Host (transport Host)", &m.hostEd, ""),
		formRow(a.th, "WS Path", &m.pathEd, ""),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(4)}.Layout(gtx,
				material.Label(a.th, a.th.TextSize,
					"Full outbound JSON — the quick fields above are patched into it on save:").Layout)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.Y = gtx.Dp(unit.Dp(210))
			gtx.Constraints.Max.Y = gtx.Dp(unit.Dp(210))
			return widget.Border{Color: colDivider, Width: unit.Dp(1), CornerRadius: unit.Dp(4)}.Layout(gtx,
				material.Editor(a.th, &m.jsonEd, "").Layout)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }),
					layout.Rigid(material.Button(a.th, &m.cancelBtn, "Cancel").Layout),
					layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
					layout.Rigid(material.Button(a.th, &m.saveBtn, "Save node").Layout),
				)
			})
		}),
	)
}

func (m *editNodeModal) save(a *App) {
	var newOb map[string]any
	if err := json.Unmarshal([]byte(m.jsonEd.Text()), &newOb); err != nil {
		a.showError(err)
		return
	}
	obSet(newOb, strings.TrimSpace(m.serverEd.Text()), "server")
	if pv := strings.TrimSpace(m.portEd.Text()); pv != "" {
		if p, err := strconv.Atoi(pv); err == nil {
			newOb["server_port"] = float64(p)
		}
	}
	obSet(newOb, strings.TrimSpace(m.sniEd.Text()), "tls", "server_name")
	// only touch Host when the outbound has a transport or the field is filled
	if strings.TrimSpace(m.hostEd.Text()) != "" || wsHostOf(newOb) != "" {
		obSet(newOb, strings.TrimSpace(m.hostEd.Text()), "transport", "headers", "Host")
	}
	if strings.TrimSpace(m.pathEd.Text()) != "" || obGet(newOb, "transport", "path") != "" {
		obSet(newOb, strings.TrimSpace(m.pathEd.Text()), "transport", "path")
	}
	name := m.nameEd.Text()
	if err := core.UpdateNode(a.store, a.mgr, a.workDir, m.nodeID, name, newOb); err != nil {
		a.showError(err)
		return
	}
	a.modal = nil
	a.logf("Node saved: %s", name)
	a.notice = ""
	if a.mgr.Running() {
		a.showConfirm("Node saved", "Restart sing-box to apply the edit?", func() {
			a.restartProxy()
		})
	}
}
