package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/Chuang2016/singbox-gui/core"
)

// Subscriptions page: the same model and semantics as the web
// dashboard -- a subscription is a named URL; adding one fetches it
// immediately, refreshing replaces its nodes, deleting removes the
// subscription and its nodes. The auto-refresh interval is the shared
// settings field cfg.AutoUpdateHr (hours, 0 = off); the GUI honours it
// with a background timer.

func (g *guiState) buildSubsPage() fyne.CanvasObject {
	g.subList = widget.NewList(
		func() int { return len(g.store.Data.Subs) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			s := g.store.Data.Subs[i]
			upd := s.UpdatedAt
			if upd == "" {
				upd = "never"
			}
			o.(*widget.Label).SetText(fmt.Sprintf("%s  ·  %d nodes  ·  updated %s  ·  %s",
				s.Name, s.Count, upd, s.URL))
		},
	)
	g.subList.OnSelected = func(id widget.ListItemID) { g.subSel = int(id) }

	addBtn := widget.NewButton("Add", func() {
		nameEntry := widget.NewEntry()
		nameEntry.SetPlaceHolder("Name")
		urlEntry := widget.NewEntry()
		urlEntry.SetPlaceHolder("https://example.com/sub")
		items := []*widget.FormItem{
			widget.NewFormItem("Name", nameEntry),
			widget.NewFormItem("URL", urlEntry),
		}
		d := dialog.NewForm("Add subscription", "Add", "Cancel", items, func(ok bool) {
			if !ok {
				return
			}
			name := strings.TrimSpace(nameEntry.Text)
			rawURL := strings.TrimSpace(urlEntry.Text)
			if rawURL == "" {
				g.fail("Add subscription: URL is empty")
				return
			}
			if name == "" {
				name = rawURL
			}
			sub := core.CreateSubscription(g.store, name, rawURL)
			g.subList.Refresh()
			g.refreshSubAsync(sub.ID)
		}, g.win)
		d.Resize(fyne.NewSize(520, 220))
		d.Show()
	})
	refreshBtn := widget.NewButton("Refresh", func() {
		if s := g.selectedSub(); s != nil {
			g.refreshSubAsync(s.ID)
		}
	})
	refreshAllBtn := widget.NewButton("Refresh all", func() {
		for _, s := range g.store.Data.Subs {
			g.refreshSubAsync(s.ID)
		}
	})
	deleteBtn := widget.NewButton("Delete", func() {
		s := g.selectedSub()
		if s == nil {
			return
		}
		dialog.ShowConfirm("Delete subscription",
			fmt.Sprintf("Delete %q and its %d nodes?", s.Name, s.Count),
			func(ok bool) {
				if !ok {
					return
				}
				core.DeleteSubscription(g.store, s.ID)
				g.subSel = 0
				g.subList.Refresh()
				if g.nodeTable != nil {
					g.nodeTable.Refresh()
				}
				g.logf("Subscription deleted: %s", s.Name)
			}, g.win)
	})

	intervalEntry := widget.NewEntry()
	intervalEntry.SetText(strconv.Itoa(g.store.Data.Cfg.AutoUpdateHr))
	intervalEntry.SetPlaceHolder("hours")
	saveIntervalBtn := widget.NewButton("Save", func() {
		h, err := strconv.Atoi(strings.TrimSpace(intervalEntry.Text))
		if err != nil || h < 0 {
			g.fail("Auto-refresh interval: enter hours >= 0 (0 = off)")
			return
		}
		g.store.Data.Cfg.AutoUpdateHr = h
		if err := g.store.Save(); err != nil {
			g.fail("Save settings: %v", err)
			return
		}
		g.lastAutoRefresh = time.Now()
		g.logf("Subscription auto-refresh interval set to %d h", h)
		g.notice = ""
		g.refreshStatus()
	})
	autoRow := container.NewBorder(nil, nil,
		widget.NewLabel("Auto-refresh every (hours, 0 = off):"), saveIntervalBtn, intervalEntry)

	buttons := container.NewHBox(addBtn, refreshBtn, refreshAllBtn, deleteBtn)
	bottom := container.NewVBox(buttons, autoRow)
	return container.NewBorder(
		widget.NewLabelWithStyle("Subscriptions", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		bottom, nil, nil, g.subList)
}

func (g *guiState) selectedSub() *core.Subscription {
	if g.subSel < 0 || g.subSel >= len(g.store.Data.Subs) {
		return nil
	}
	return &g.store.Data.Subs[g.subSel]
}

// refreshSubAsync refetches one subscription off the UI thread and
// reports the outcome in the log/status bar.
func (g *guiState) refreshSubAsync(id string) {
	name := id
	for _, s := range g.store.Data.Subs {
		if s.ID == id {
			name = s.Name
		}
	}
	go func() {
		n, err := core.RefreshSubscription(g.store, id)
		fyne.Do(func() {
			if err != nil {
				g.fail("Subscription %s refresh failed: %v", name, err)
			} else {
				g.logf("Subscription %s refreshed: %d nodes", name, n)
				g.notice = ""
			}
			if g.subList != nil {
				g.subList.Refresh()
			}
			if g.nodeTable != nil {
				g.nodeTable.Refresh()
			}
			g.refreshStatus()
		})
	}()
}

// maybeAutoRefreshSubs runs on the 1s tick: when the configured
// interval has elapsed, refresh every subscription once.
func (g *guiState) maybeAutoRefreshSubs() {
	h := g.store.Data.Cfg.AutoUpdateHr
	if h <= 0 || len(g.store.Data.Subs) == 0 {
		return
	}
	if g.lastAutoRefresh.IsZero() {
		g.lastAutoRefresh = time.Now()
		return
	}
	if time.Since(g.lastAutoRefresh) < time.Duration(h)*time.Hour {
		return
	}
	g.lastAutoRefresh = time.Now()
	g.logf("Auto-refreshing %d subscriptions", len(g.store.Data.Subs))
	for _, s := range g.store.Data.Subs {
		g.refreshSubAsync(s.ID)
	}
}
