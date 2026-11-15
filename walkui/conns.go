//go:build windows

package main

import (
	"time"

	"github.com/lxn/walk"

	"github.com/Chuang2016/singbox-gui/core"
)

// Connections page: the dashboard's Active connections table,
// natively. Rows come from core.FetchConnections (Clash API); the
// page auto-refreshes every 2 s while it is the visible tab and
// sing-box is running.

type connModel struct {
	walk.TableModelBase
	a *app
}

func (m *connModel) RowCount() int { return len(m.a.conns) }

func (m *connModel) Value(row, col int) interface{} {
	if row < 0 || row >= len(m.a.conns) {
		return ""
	}
	c := m.a.conns[row]
	switch col {
	case 0:
		return c.Host
	case 1:
		return c.Process
	case 2:
		return c.Outbound
	case 3:
		return humanBytes(c.Upload)
	case 4:
		return humanBytes(c.Download)
	case 5:
		return c.Rule
	}
	return ""
}

// fetchConnsAsync reloads the connection list in the background.
// force bypasses the 2 s throttle used by the ticker.
func (a *app) fetchConnsAsync(force bool) {
	if !a.mgr.Running() {
		if len(a.conns) > 0 {
			a.conns = nil
			a.connModel.PublishRowsReset()
		}
		if a.connInfoLbl != nil {
			a.connInfoLbl.SetText("sing-box is not running.")
		}
		return
	}
	if !force && time.Since(a.lastConnFetch) < 2*time.Second {
		return
	}
	a.lastConnFetch = time.Now()
	port := a.store.Data.Cfg.ClashPort
	go func() {
		conns, err := core.FetchConnections(port)
		a.mw.Synchronize(func() {
			if err != nil {
				if a.connInfoLbl != nil {
					a.connInfoLbl.SetText("Connections unavailable: " + err.Error())
				}
				return
			}
			// newest first, capped like the dashboard
			for i, j := 0, len(conns)-1; i < j; i, j = i+1, j-1 {
				conns[i], conns[j] = conns[j], conns[i]
			}
			if len(conns) > 100 {
				conns = conns[:100]
			}
			a.conns = conns
			a.connModel.PublishRowsReset()
			if a.connInfoLbl != nil {
				a.connInfoLbl.SetText("Live connections via the Clash API (auto-refreshes on this tab).")
			}
		})
	}()
}

// killSelectedConn closes the highlighted connection.
func (a *app) killSelectedConn() {
	if a.connTable == nil {
		return
	}
	row := a.connTable.CurrentIndex()
	if row < 0 || row >= len(a.conns) {
		a.fail("Connections: select a connection first")
		return
	}
	c := a.conns[row]
	if err := core.KillConnection(a.store.Data.Cfg.ClashPort, c.ID); err != nil {
		a.fail("Kill connection failed: %v", err)
		return
	}
	a.logf("Closed connection to %s", c.Host)
	a.conns = append(a.conns[:row], a.conns[row+1:]...)
	a.connModel.PublishRowsReset()
	a.refreshLogs()
}
