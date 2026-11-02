//go:build windows

package main

import (
	"fmt"
	"time"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"

	"github.com/Chuang2016/singbox-gui/core"
)

// Subscriptions in the front-end are deliberately thin: list, add and
// refresh (plus the same auto-refresh the settings interval asks
// for). Full management -- delete, rename, per-sub inspection --
// lives in the web back-end; nothing here forks core's logic
// (core.CreateSubscription / core.RefreshSubscription).

type subModel struct {
	walk.ListModelBase
	a *app
}

func (m *subModel) ItemCount() int {
	return len(m.a.store.Data.Subs)
}

func (m *subModel) Value(index int) interface{} {
	subs := m.a.store.Data.Subs
	if index < 0 || index >= len(subs) {
		return ""
	}
	s := subs[index]
	return fmt.Sprintf("%s (%d)", s.Name, s.Count)
}

// initSubRail fills the left rail (created empty by buildWindow)
// with the subscription list and its buttons.
func (a *app) initSubRail() {
	if a.railBox == nil {
		return
	}
	a.subModel = &subModel{a: a}

	lb, err := walk.NewListBox(a.railBox)
	if err == nil {
		a.subList = lb
		lb.SetModel(a.subModel)
	}

	row, err := walk.NewComposite(a.railBox)
	if err == nil {
		_ = row.SetLayout(walk.NewHBoxLayout())
		mkBtn := func(text string, fn func()) {
			b, err := walk.NewPushButton(row)
			if err == nil {
				_ = b.SetText(text)
				b.Clicked().Attach(fn)
			}
		}
		mkBtn("Add", func() { a.addSubscription() })
		mkBtn("Refresh", func() { a.refreshSelectedSub() })
		mkBtn("All", func() { a.refreshAllSubs() })
	}

	hint, err := walk.NewLabel(a.railBox)
	if err == nil {
		_ = hint.SetText("Full subscription and rule\nmanagement: web back-end.")
	}
}

// selectedSub returns the highlighted subscription, if any.
func (a *app) selectedSub() *core.Subscription {
	if a.subList == nil {
		return nil
	}
	i := a.subList.CurrentIndex()
	subs := a.store.Data.Subs
	if i < 0 || i >= len(subs) {
		return nil
	}
	return &subs[i]
}

func (a *app) refreshSelectedSub() {
	sub := a.selectedSub()
	if sub == nil {
		a.fail("Subscriptions: select a subscription first")
		return
	}
	a.refreshSubAsync(sub.ID, sub.Name)
}

func (a *app) refreshAllSubs() {
	subs := a.store.Data.Subs
	if len(subs) == 0 {
		a.fail("Subscriptions: none to refresh")
		return
	}
	a.logf("Refreshing %d subscriptions...", len(subs))
	for _, s := range subs {
		a.refreshSubAsync(s.ID, s.Name)
	}
}

// refreshSubAsync refetches one subscription in the background and
// posts the outcome back to the UI goroutine.
func (a *app) refreshSubAsync(id, name string) {
	go func() {
		n, err := core.RefreshSubscription(a.store, id)
		a.mw.Synchronize(func() {
			if err != nil {
				a.fail("Subscription %s refresh failed: %v", name, err)
				return
			}
			a.logf("Subscription %s refreshed: %d nodes", name, n)
			a.notice = ""
			a.refreshStatus()
			if a.subModel != nil {
				a.subModel.PublishItemsReset()
			}
			a.lastNodeCount = len(a.store.Data.Nodes)
			a.model.PublishRowsReset()
		})
	}()
}

// addSubscription asks for name + URL in a small modal dialog, then
// creates and immediately refreshes the subscription.
func (a *app) addSubscription() {
	var dlg *walk.Dialog
	var nameEd, urlEd *walk.LineEdit
	var accept *walk.PushButton
	def := Dialog{
		AssignTo:      &dlg,
		Title:         "Add subscription",
		DefaultButton: &accept,
		MinSize:       Size{Width: 420, Height: 170},
		Layout:        VBox{Margins: Margins{Left: 10, Top: 10, Right: 10, Bottom: 10}, Spacing: 8},
		Children: []Widget{
			Label{Text: "Name (optional):"},
			LineEdit{AssignTo: &nameEd},
			Label{Text: "Subscription URL:"},
			LineEdit{AssignTo: &urlEd},
			Composite{
				Layout: HBox{MarginsZero: true, Spacing: 6},
				Children: []Widget{
					HSpacer{},
					PushButton{AssignTo: &accept, Text: "Add"},
					PushButton{Text: "Cancel", OnClicked: func() { dlg.Cancel() }},
				},
			},
		},
	}
	if err := def.Create(a.mw); err != nil {
		a.fail("Add subscription: %v", err)
		return
	}
	accept.Clicked().Attach(func() {
		if urlEd.Text() == "" {
			return
		}
		dlg.Accept()
	})
	if dlg.Run() != walk.DlgCmdOK {
		return
	}
	name := nameEd.Text()
	rawURL := urlEd.Text()
	if name == "" {
		name = rawURL
	}
	sub := core.CreateSubscription(a.store, name, rawURL)
	if a.subModel != nil {
		a.subModel.PublishItemsReset()
	}
	a.logf("Subscription added: %s", name)
	a.refreshSubAsync(sub.ID, sub.Name)
}

// maybeAutoRefreshSubs runs on the 1s tick: when the configured
// interval (Settings.AutoUpdateHr, shared with the web back-end's
// settings) has elapsed, refresh every subscription once. Model
// carried over from the frozen Fyne front-end.
func (a *app) maybeAutoRefreshSubs() {
	h := a.store.Data.Cfg.AutoUpdateHr
	if h <= 0 || len(a.store.Data.Subs) == 0 {
		return
	}
	if a.lastAutoRefresh.IsZero() {
		a.lastAutoRefresh = time.Now()
		return
	}
	if time.Since(a.lastAutoRefresh) < time.Duration(h)*time.Hour {
		return
	}
	a.lastAutoRefresh = time.Now()
	a.logf("Auto-refreshing %d subscriptions", len(a.store.Data.Subs))
	for _, s := range a.store.Data.Subs {
		a.refreshSubAsync(s.ID, s.Name)
	}
}
