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
// connections card proxies). Read-only, auto-refreshed every two
// seconds while the page is visible and sing-box is running, newest
// first, capped at 100 rows.

var connColumns = []struct {
	title string
	width float32
}{
	{"Process", 170},
	{"Host", 320},
	{"Rule", 250},
	{"Outbound", 110},
	{"Up", 90},
	{"Down", 90},
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
				lbl.SetText(c.Process)
			case 1:
				lbl.SetText(c.Host)
			case 2:
				lbl.SetText(c.Rule)
			case 3:
				lbl.SetText(c.Outbound)
			case 4:
				lbl.SetText(humanBytes(c.Upload))
			case 5:
				lbl.SetText(humanBytes(c.Download))
			}
		},
	)
	for i, c := range connColumns {
		g.connTable.SetColumnWidth(i, c.width)
	}
	return container.NewBorder(
		container.NewVBox(
			widget.NewLabelWithStyle("Connections", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			g.connNote,
		),
		nil, nil, nil, g.connTable)
}

// maybeFetchConns reloads the connection list off the UI thread. It
// is called from the per-second refresh; fetches run at most every
// other tick and only while the page could be visible.
func (g *guiState) maybeFetchConns() {
	if g.current != pageConns {
		return
	}
	if !g.mgr.Running() {
		if len(g.conns) > 0 {
			g.conns = nil
		}
		if g.connNote != nil {
			g.connNote.SetText("sing-box is not running")
		}
		return
	}
	if g.ticks%2 != 0 || g.connFetching {
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
