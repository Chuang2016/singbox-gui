package main

import (
	"fmt"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/Chuang2016/singbox-gui/core"
)

// Connections page: the live connection table from sing-box's Clash
// API (core.FetchConnections). Auto-refreshed every two seconds
// while the page is visible and sing-box is running, newest first,
// capped at 100 rows; the selected row can be closed with
// "Kill selected" (core.KillConnection). Mirrors gui/connections.go.

var connColumns = []struct {
	title string
	width unit.Dp
}{
	{"Destination", 280},
	{"Process", 140},
	{"Node", 120},
	{"Up", 85},
	{"Down", 85},
	{"Rule", 220},
}

const maxConnRows = 100

type connsPage struct {
	list      widget.List
	rowClicks []widget.Clickable
	refreshB  widget.Clickable
	killB     widget.Clickable

	conns     []core.Connection
	note      string
	fetching  bool
	sel       int // selected data row, -1 = none
	lastFetch time.Time
}

func (p *connsPage) init() {
	p.list.List.Axis = layout.Vertical
	p.sel = -1
	p.note = "sing-box is not running"
}

func (p *connsPage) layout(gtx layout.Context, a *App) layout.Dimensions {
	if p.refreshB.Clicked(gtx) {
		p.fetch(a, true)
	}
	if p.killB.Clicked(gtx) {
		p.killSelected(a)
	}
	conns := p.conns
	for len(p.rowClicks) < len(conns) {
		p.rowClicks = append(p.rowClicks, widget.Clickable{})
	}
	return layout.Inset{Top: unit.Dp(10), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Label(a.th, unit.Sp(15), "Connections")
					l.Font.Weight = font.Bold
					return l.Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(4)}.Layout(gtx,
						func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(material.Button(a.th, &p.refreshB, "Refresh").Layout),
								layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
								layout.Rigid(material.Button(a.th, &p.killB, "Kill selected").Layout),
								layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
								layout.Rigid(material.Label(a.th, a.th.TextSize, p.note).Layout),
							)
						})
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return material.List(a.th, &p.list).Layout(gtx, len(conns)+1,
						func(gtx layout.Context, idx int) layout.Dimensions {
							if idx == 0 {
								children := make([]layout.FlexChild, 0, len(connColumns))
								for col := range connColumns {
									children = append(children, layout.Rigid(
										cellLabel(a.th, connColumns[col].width, connColumns[col].title, true)))
								}
								return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(4)}.Layout(gtx,
									func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, children...)
									})
							}
							row := idx - 1
							c := conns[row]
							if p.rowClicks[row].Clicked(gtx) {
								p.sel = row
							}
							content := func(gtx layout.Context) layout.Dimensions {
								return material.Clickable(gtx, &p.rowClicks[row], func(gtx layout.Context) layout.Dimensions {
									return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
										layout.Rigid(cellLabel(a.th, connColumns[0].width, c.Host, false)),
										layout.Rigid(cellLabel(a.th, connColumns[1].width, c.Process, false)),
										layout.Rigid(cellLabel(a.th, connColumns[2].width, c.Outbound, false)),
										layout.Rigid(cellLabel(a.th, connColumns[3].width, humanBytes(c.Upload), false)),
										layout.Rigid(cellLabel(a.th, connColumns[4].width, humanBytes(c.Download), false)),
										layout.Rigid(cellLabel(a.th, connColumns[5].width, c.Rule, false)),
									)
								})
							}
							return layout.Inset{Top: unit.Dp(3), Bottom: unit.Dp(3)}.Layout(gtx,
								func(gtx layout.Context) layout.Dimensions {
									if row == p.sel {
										return layout.Stack{}.Layout(gtx,
											layout.Expanded(bgWidget(colRowSel)),
											layout.Stacked(content),
										)
									}
									return content(gtx)
								})
						})
				}),
			)
		})
}

// killSelected closes the highlighted connection through the Clash
// API.
func (p *connsPage) killSelected(a *App) {
	if p.sel < 0 || p.sel >= len(p.conns) {
		a.fail("Connections: select a connection first")
		return
	}
	c := p.conns[p.sel]
	if err := core.KillConnection(a.store.Data.Cfg.ClashPort, c.ID); err != nil {
		a.fail("Kill connection failed: %v", err)
		return
	}
	a.logf("Closed connection to %s", c.Host)
	p.conns = append(p.conns[:p.sel], p.conns[p.sel+1:]...)
	p.sel = -1
}

// maybeFetch is the per-second-tick entry point: fetch at most
// every two seconds while the page is visible.
func (p *connsPage) maybeFetch(a *App) {
	p.fetch(a, false)
}

// fetch reloads the connection list off the UI goroutine. force
// bypasses the two-second throttle (the Refresh button).
func (p *connsPage) fetch(a *App, force bool) {
	if !a.mgr.Running() {
		if len(p.conns) > 0 {
			p.conns = nil
		}
		p.note = "sing-box is not running"
		return
	}
	if p.fetching || (!force && time.Since(p.lastFetch) < 2*time.Second) {
		return
	}
	p.fetching = true
	p.lastFetch = time.Now()
	go func() {
		conns, err := core.FetchConnections(a.store.Data.Cfg.ClashPort)
		a.post(func() {
			p.fetching = false
			if err != nil {
				p.note = fmt.Sprintf("Clash API unreachable: %v", err)
				return
			}
			// newest first, like gui/ (it reverses the tail)
			for i, j := 0, len(conns)-1; i < j; i, j = i+1, j-1 {
				conns[i], conns[j] = conns[j], conns[i]
			}
			if len(conns) > maxConnRows {
				conns = conns[:maxConnRows]
			}
			p.conns = conns
			if p.sel >= len(conns) {
				p.sel = -1
			}
			p.note = fmt.Sprintf("%d live connections · updated %s",
				len(conns), time.Now().Format("15:04:05"))
		})
	}()
}
