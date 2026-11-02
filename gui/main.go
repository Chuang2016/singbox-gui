// sbgui-gui: native Fyne front-end for the sbgui core engine (milestone 1 PoC).
//
// This is a proof of concept: it reuses core/ as-is (store, link parsing,
// config building, sing-box process management) and replaces the browser
// dashboard with a real window + system tray. Feature parity with the web
// dashboard (subscriptions, rules editor, traffic charts, connections table,
// system-proxy toggle, urltest) is future work -- see MILESTONE1.md.
package main

import (
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"github.com/Chuang2016/singbox-gui/core"
)

// demoLinks seed the list (in memory only, never saved) when the store is
// empty, so the PoC shows the real core.ParseShareLink path on first run.
var demoLinks = []string{
	"vless://00000000-0000-0000-0000-000000000001@example.com:443?security=tls&sni=example.com#Demo-VLESS",
	"trojan://demopass@example.com:443?sni=example.com#Demo-Trojan",
	"hysteria2://demopass@example.com:443?insecure=1#Demo-HY2",
}

type guiState struct {
	workDir string
	store   *core.Store
	mgr     *core.Manager

	status *widget.Label
	logs   *widget.Label
	list   *widget.List
	connBt *widget.Button
}

func (g *guiState) activeNode() *core.Node {
	for i := range g.store.Data.Nodes {
		if g.store.Data.Nodes[i].ID == g.store.Data.Cfg.ActiveNodeID {
			return &g.store.Data.Nodes[i]
		}
	}
	if len(g.store.Data.Nodes) > 0 {
		return &g.store.Data.Nodes[0]
	}
	return nil
}

func (g *guiState) refreshStatus() {
	state := "Stopped"
	if g.mgr.Running() {
		state = "Running"
		g.connBt.SetText("Disconnect")
	} else {
		g.connBt.SetText("Connect")
	}
	node := "none"
	if n := g.activeNode(); n != nil {
		node = n.Tag
	}
	g.status.SetText(fmt.Sprintf("Status: %s  |  Active node: %s  |  Mixed port: %d  |  Mode: %s",
		state, node, g.store.Data.Cfg.MixedPort, g.store.Data.Cfg.Mode))
	lines := g.mgr.Logs()
	if len(lines) > 12 {
		lines = lines[len(lines)-12:]
	}
	g.logs.SetText(strings.Join(lines, "\n"))
}

// startProxy mirrors server.Server.StartProxy's orchestration using only
// core: refresh rule-sets (non-fatal), build config, start the subprocess.
func (g *guiState) startProxy() error {
	if err := core.EnsureRuleSets(g.workDir); err != nil {
		// v1.0.8 behaviour: keep cached rule-sets and carry on.
		fmt.Println("rulesets:", err)
	}
	if _, err := core.BuildConfig(g.store.Data, g.workDir); err != nil {
		return err
	}
	return g.mgr.Start()
}

func (g *guiState) toggle() {
	var err error
	if g.mgr.Running() {
		err = g.mgr.Stop()
	} else {
		if n := g.activeNode(); n != nil {
			g.store.Data.Cfg.ActiveNodeID = n.ID
		}
		err = g.startProxy()
	}
	if err != nil {
		g.status.SetText("Error: " + err.Error())
		return
	}
	g.refreshStatus()
}

func main() {
	a := app.NewWithID("org.sbgui.native")
	w := a.NewWindow("sbgui — sing-box manager (native PoC)")
	w.Resize(fyne.NewSize(760, 520))

	workDir := core.WorkDir()
	store, err := core.OpenStore(workDir)
	if err != nil {
		w.SetContent(widget.NewLabel("Failed to open store: " + err.Error()))
		w.ShowAndRun()
		return
	}
	seeded := false
	if len(store.Data.Nodes) == 0 {
		for _, link := range demoLinks {
			proto, ob, err := core.ParseShareLink(link)
			if err != nil {
				continue
			}
			store.Data.Nodes = append(store.Data.Nodes, core.Node{
				ID: core.NewID("n"), Tag: core.LinkTag(link, proto),
				Protocol: proto, Outbound: ob, Source: "manual",
			})
			seeded = true
		}
		if seeded && store.Data.Cfg.ActiveNodeID == "" && len(store.Data.Nodes) > 0 {
			store.Data.Cfg.ActiveNodeID = store.Data.Nodes[0].ID
		}
	}

	g := &guiState{workDir: workDir, store: store, mgr: core.NewManager(workDir)}
	g.status = widget.NewLabel("")
	g.logs = widget.NewLabel("")
	g.logs.Wrapping = fyne.TextWrapBreak
	g.connBt = widget.NewButton("Connect", g.toggle)

	g.list = widget.NewList(
		func() int { return len(g.store.Data.Nodes) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			n := g.store.Data.Nodes[i]
			mark := ""
			if n.ID == g.store.Data.Cfg.ActiveNodeID {
				mark = "  [active]"
			}
			o.(*widget.Label).SetText(fmt.Sprintf("%s  ·  %s%s", n.Tag, n.Protocol, mark))
		},
	)
	g.list.OnSelected = func(id widget.ListItemID) {
		g.store.Data.Cfg.ActiveNodeID = g.store.Data.Nodes[id].ID
		_ = g.store.Save()
		g.refreshStatus()
		g.list.Refresh()
	}

	importEntry := widget.NewEntry()
	importEntry.SetPlaceHolder("Paste a share link (vless://, trojan://, hysteria2://, ...) and press Import")
	importBtn := widget.NewButton("Import link", func() {
		link := strings.TrimSpace(importEntry.Text)
		if link == "" {
			return
		}
		proto, ob, err := core.ParseShareLink(link)
		if err != nil {
			g.status.SetText("Import failed: " + err.Error())
			return
		}
		g.store.Data.Nodes = append(g.store.Data.Nodes, core.Node{
			ID: core.NewID("n"), Tag: core.LinkTag(link, proto),
			Protocol: proto, Outbound: ob, Source: "manual",
		})
		_ = g.store.Save()
		importEntry.SetText("")
		g.list.Refresh()
		g.refreshStatus()
	})

	header := container.NewVBox(
		widget.NewLabelWithStyle("sbgui native GUI — milestone 1 PoC", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		g.status,
	)
	bottom := container.NewVBox(
		container.NewBorder(nil, nil, nil, importBtn, importEntry),
		g.connBt,
		widget.NewLabelWithStyle("sing-box log", fyne.TextAlignLeading, fyne.TextStyle{Italic: true}),
		container.NewVScroll(g.logs),
	)
	w.SetContent(container.NewBorder(header, bottom, nil, nil, g.list))
	g.refreshStatus()
	if seeded {
		g.status.SetText(g.status.Text + "  |  (demo nodes seeded — store was empty)")
	}

	// System tray: Show / Quit. Closing the window hides it to the tray.
	if desk, ok := a.(desktop.App); ok {
		menu := fyne.NewMenu("sbgui",
			fyne.NewMenuItem("Show", func() { w.Show() }),
			fyne.NewMenuItem("Quit", func() { a.Quit() }),
		)
		desk.SetSystemTrayMenu(menu)
		w.SetCloseIntercept(func() { w.Hide() })
	}

	// Poll process state + logs once a second.
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for range t.C {
			fyne.Do(g.refreshStatus)
		}
	}()

	w.ShowAndRun()
}
