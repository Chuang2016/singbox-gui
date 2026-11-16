package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/Chuang2016/singbox-gui/core"
)

// Subscriptions page: same model and semantics as gui/subs.go — a
// subscription is a named URL; adding one fetches it immediately,
// refreshing replaces its nodes, deleting removes the subscription
// and its nodes. The auto-refresh interval is the shared settings
// field cfg.AutoUpdateHr (hours, 0 = off), honoured by a background
// check on the 1s tick.

type subsPage struct {
	list      widget.List
	rowClicks []widget.Clickable
	sel       int

	addBtn    widget.Clickable
	updBtn    widget.Clickable
	updAllBtn widget.Clickable
	delBtn    widget.Clickable

	intervalEd   widget.Editor
	intervalBtn  widget.Clickable
	intervalInit bool

	lastAutoRefresh time.Time
}

func (p *subsPage) init() {
	p.list.List.Axis = layout.Vertical
	p.intervalEd.SingleLine = true
	p.intervalEd.Submit = true
}

func (p *subsPage) selected(a *App) *core.Subscription {
	if p.sel < 0 || p.sel >= len(a.store.Data.Subs) {
		return nil
	}
	return &a.store.Data.Subs[p.sel]
}

func (p *subsPage) layout(gtx layout.Context, a *App) layout.Dimensions {
	if p.addBtn.Clicked(gtx) {
		a.modal = &addSubModal{}
	}
	if p.updBtn.Clicked(gtx) {
		if s := p.selected(a); s != nil {
			p.refreshAsync(a, s.ID)
		}
	}
	if p.updAllBtn.Clicked(gtx) {
		for _, s := range a.store.Data.Subs {
			p.refreshAsync(a, s.ID)
		}
	}
	if p.delBtn.Clicked(gtx) {
		if s := p.selected(a); s != nil {
			sub := *s
			a.showConfirm("Delete subscription",
				fmt.Sprintf("Delete %q and its %d nodes?", sub.Name, sub.Count),
				func() {
					core.DeleteSubscription(a.store, sub.ID)
					p.sel = 0
					a.logf("Subscription deleted: %s", sub.Name)
				})
		}
	}
	saveInterval := func() {
		h, err := strconv.Atoi(strings.TrimSpace(p.intervalEd.Text()))
		if err != nil || h < 0 {
			a.fail("Auto-refresh interval: enter hours >= 0 (0 = off)")
			return
		}
		a.store.Data.Cfg.AutoUpdateHr = h
		if err := a.store.Save(); err != nil {
			a.fail("Save settings: %v", err)
			return
		}
		p.lastAutoRefresh = time.Now()
		a.logf("Subscription auto-refresh interval set to %d h", h)
		a.notice = ""
	}
	if p.intervalBtn.Clicked(gtx) {
		saveInterval()
	}
	for {
		ev, ok := p.intervalEd.Update(gtx)
		if !ok {
			break
		}
		if _, isSubmit := ev.(widget.SubmitEvent); isSubmit {
			saveInterval()
		}
	}
	// Initialise the interval entry once (the Rules page entry
	// writes the same settings field).
	if !p.intervalInit {
		p.intervalInit = true
		p.intervalEd.SetText(strconv.Itoa(a.store.Data.Cfg.AutoUpdateHr))
	}

	subs := a.store.Data.Subs
	for len(p.rowClicks) < len(subs) {
		p.rowClicks = append(p.rowClicks, widget.Clickable{})
	}
	return layout.Inset{Top: unit.Dp(10), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Label(a.th, unit.Sp(15), "Subscriptions")
					l.Font.Weight = font.Bold
					return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, l.Layout)
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return material.List(a.th, &p.list).Layout(gtx, len(subs),
						func(gtx layout.Context, i int) layout.Dimensions {
							s := subs[i]
							if p.rowClicks[i].Clicked(gtx) {
								p.sel = i
							}
							upd := s.UpdatedAt
							if upd == "" {
								upd = "never"
							}
							text := fmt.Sprintf("%s  ·  %d nodes  ·  updated %s  ·  %s",
								s.Name, s.Count, upd, s.URL)
							content := func(gtx layout.Context) layout.Dimensions {
								return material.Clickable(gtx, &p.rowClicks[i],
									material.Label(a.th, a.th.TextSize, text).Layout)
							}
							return layout.Inset{Top: unit.Dp(3), Bottom: unit.Dp(3)}.Layout(gtx,
								func(gtx layout.Context) layout.Dimensions {
									if i == p.sel {
										return layout.Stack{}.Layout(gtx,
											layout.Expanded(bgWidget(colRowSel)),
											layout.Stacked(content),
										)
									}
									return content(gtx)
								})
						})
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
							layout.Rigid(material.Button(a.th, &p.addBtn, "Add").Layout),
							layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
							layout.Rigid(material.Button(a.th, &p.updBtn, "Update").Layout),
							layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
							layout.Rigid(material.Button(a.th, &p.updAllBtn, "Update all").Layout),
							layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
							layout.Rigid(material.Button(a.th, &p.delBtn, "Delete").Layout),
						)
					})
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8)}.Layout(gtx,
						func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(material.Label(a.th, a.th.TextSize, "Auto-refresh every (hours, 0 = off):").Layout),
								layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									gtx.Constraints.Min.X = gtx.Dp(unit.Dp(70))
									gtx.Constraints.Max.X = gtx.Dp(unit.Dp(70))
									return material.Editor(a.th, &p.intervalEd, "hours").Layout(gtx)
								}),
								layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
								layout.Rigid(material.Button(a.th, &p.intervalBtn, "Save").Layout),
							)
						})
				}),
			)
		})
}

// refreshAsync refetches one subscription off the UI goroutine and
// reports the outcome in the log/status bar.
func (p *subsPage) refreshAsync(a *App, id string) {
	name := id
	for _, s := range a.store.Data.Subs {
		if s.ID == id {
			name = s.Name
		}
	}
	go func() {
		n, err := core.RefreshSubscription(a.store, id)
		a.post(func() {
			if err != nil {
				a.fail("Subscription %s refresh failed: %v", name, err)
			} else {
				a.logf("Subscription %s refreshed: %d nodes", name, n)
				a.notice = ""
			}
		})
	}()
}

// maybeAutoRefresh runs on the 1s tick: when the configured
// interval has elapsed, refresh every subscription once.
func (p *subsPage) maybeAutoRefresh(a *App) {
	h := a.store.Data.Cfg.AutoUpdateHr
	if h <= 0 || len(a.store.Data.Subs) == 0 {
		return
	}
	if p.lastAutoRefresh.IsZero() {
		p.lastAutoRefresh = time.Now()
		return
	}
	if time.Since(p.lastAutoRefresh) < time.Duration(h)*time.Hour {
		return
	}
	p.lastAutoRefresh = time.Now()
	a.logf("Auto-refreshing %d subscriptions", len(a.store.Data.Subs))
	for _, s := range a.store.Data.Subs {
		p.refreshAsync(a, s.ID)
	}
}

// ---------- add-subscription modal ----------

type addSubModal struct {
	nameEd    widget.Editor
	urlEd     widget.Editor
	addBtn    widget.Clickable
	cancelBtn widget.Clickable
	init      bool
}

func (m *addSubModal) layoutModal(gtx layout.Context, a *App) layout.Dimensions {
	if !m.init {
		m.init = true
		m.nameEd.SingleLine = true
		m.urlEd.SingleLine = true
	}
	if m.cancelBtn.Clicked(gtx) {
		a.modal = nil
		return layout.Dimensions{}
	}
	if m.addBtn.Clicked(gtx) {
		name := strings.TrimSpace(m.nameEd.Text())
		rawURL := strings.TrimSpace(m.urlEd.Text())
		if rawURL == "" {
			a.modal = nil
			a.fail("Add subscription: URL is empty")
			return layout.Dimensions{}
		}
		if name == "" {
			name = rawURL
		}
		sub := core.CreateSubscription(a.store, name, rawURL)
		a.modal = nil
		a.subs.refreshAsync(a, sub.ID)
		return layout.Dimensions{}
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(modalTitle(a.th, "Add subscription")),
		formRow(a.th, "Name", &m.nameEd, "Name"),
		formRow(a.th, "URL", &m.urlEd, "https://example.com/sub"),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }),
					layout.Rigid(material.Button(a.th, &m.cancelBtn, "Cancel").Layout),
					layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
					layout.Rigid(material.Button(a.th, &m.addBtn, "Add").Layout),
				)
			})
		}),
	)
}
