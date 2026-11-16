package main

import (
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/Chuang2016/singbox-gui/core"
)

// Connections page: the live connection table from sing-box's Clash
// API (core.FetchConnections -- the same data the web dashboard's
// connections card proxies). Auto-refreshed every two seconds while
// the page is visible and sing-box is running, newest first, capped
// at 100 rows; the selected row can be closed with "Kill selected"
// (core.KillConnection), like the Walk front-end.

var connColumns = []struct {
	title string
	width float32
}{
	{"Destination", 300},
	{"Process", 150},
	{"Node", 130},
	{"Up", 90},
	{"Down", 90},
	{"Rule", 240},
}

const maxConnRows = 100

func (g *guiState) buildConnsPage() fyne.CanvasObject {
	g.connNote = widget.NewLabel("sing-box is not running")
	g.connTable = widget.NewTable(
		func() (int, int) { return len(g.conns) + 1, len(connColumns) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.TableCellID, o fyne.CanvasObject) {
			lbl := o.(*widget.Label)
			if id.Row == 0 {
				lbl.TextStyle = fyne.TextStyle{Bold: true}
				lbl.SetText(connColumns[id.Col].title)
				return
			}
			lbl.TextStyle = fyne.TextStyle{}
			c := g.conns[id.Row-1]
			switch id.Col {
			case 0:
				lbl.SetText(c.Host)
			case 1:
				lbl.SetText(c.Process)
			case 2:
				lbl.SetText(c.Outbound)
			case 3:
				lbl.SetText(humanBytes(c.Upload))
			case 4:
				lbl.SetText(humanBytes(c.Download))
			case 5:
				lbl.SetText(c.Rule)
			}
		},
	)
	for i, c := range connColumns {
		g.connTable.SetColumnWidth(i, c.width)
	}
	g.connTable.OnSelected = func(id widget.TableCellID) {
		if id.Row >= 1 {
			g.connSel = id.Row - 1
		}
	}
	refreshBtn := widget.NewButton("Refresh", func() { g.fetchConns(true) })
	killBtn := widget.NewButton("Kill selected", func() { g.killSelectedConn() })
	return container.NewBorder(
		container.NewVBox(
			widget.NewLabelWithStyle("Connections", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			container.NewHBox(refreshBtn, killBtn),
			g.connNote,
		),
		nil, nil, nil, g.connTable)
}

// killSelectedConn closes the highlighted connection through the
// Clash API (the Walk front-end's Kill selected).
func (g *guiState) killSelectedConn() {
	if g.connSel < 0 || g.connSel >= len(g.conns) {
		g.fail("Connections: select a connection first")
		return
	}
	c := g.conns[g.connSel]
	if err := core.KillConnection(g.store.Data.Cfg.ClashPort, c.ID); err != nil {
		g.fail("Kill connection failed: %v", err)
		return
	}
	g.logf("Closed connection to %s", c.Host)
	g.conns = append(g.conns[:g.connSel], g.conns[g.connSel+1:]...)
	g.connSel = -1
	if g.connTable != nil {
		g.connTable.Refresh()
	}
}

// maybeFetchConns is the per-second-refresh entry point: it fetches
// at most every other tick and only while the page could be visible.
func (g *guiState) maybeFetchConns() {
	if g.current != pageConns {
		return
	}
	g.fetchConns(false)
}

// fetchConns reloads the connection list off the UI thread. force
// bypasses the every-other-tick throttle (the Refresh button).
func (g *guiState) fetchConns(force bool) {
	if !g.mgr.Running() {
		if len(g.conns) > 0 {
			g.conns = nil
		}
		if g.connNote != nil {
			g.connNote.SetText("sing-box is not running")
		}
		return
	}
	if (!force && g.ticks%2 != 0) || g.connFetching {
		return
	}
	g.connFetching = true
	go func() {
		conns, err := core.FetchConnections(g.store.Data.Cfg.ClashPort)
		fyne.Do(func() {
			g.connFetching = false
			if err != nil {
				if g.connNote != nil {
					g.connNote.SetText(fmt.Sprintf("Clash API unreachable: %v", err))
				}
				return
			}
			// newest first, like the dashboard (it reverses the tail)
			for i, j := 0, len(conns)-1; i < j; i, j = i+1, j-1 {
				conns[i], conns[j] = conns[j], conns[i]
			}
			if len(conns) > maxConnRows {
				conns = conns[:maxConnRows]
			}
			g.conns = conns
			if g.connNote != nil {
				g.connNote.SetText(fmt.Sprintf("%d live connections · updated %s",
					len(conns), time.Now().Format("15:04:05")))
			}
			if g.connTable != nil {
				g.connTable.Refresh()
			}
		})
	}()
}
