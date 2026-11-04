//go:build windows

// sbgui Windows front-end: a lightweight native (Walk / Win32) window
// in front of the same core + server the web dashboard uses.
//
// One process, two faces:
//   - this Walk window for day-to-day operation (node table, connect,
//     system-proxy toggle, mode, latency, subscriptions), and
//   - the full web back-end on http://127.0.0.1:20123, started
//     in-process, for everything else (rules editor, connections,
//     speed tests, settings) -- open it in any browser, any time.
//
// Both faces share one store and one sing-box child. The Fyne GUI in
// ../gui is frozen and not part of this build.
package main

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"

	"github.com/lxn/walk"

	"github.com/Chuang2016/singbox-gui/core"
	"github.com/Chuang2016/singbox-gui/server"
)

// version of the native front-end build (preview line).
const version = "1.1.0-gui-preview.4"

func fatalBox(title, text string) {
	walk.MsgBox(nil, title, text, walk.MsgBoxIconError|walk.MsgBoxOK)
}

func main() {
	port := flag.Int("port", 20123, "web back-end port (127.0.0.1)")
	flag.Parse()

	// One front-end per machine: a second double-click just raises
	// the window of the instance that is already running.
	showEvt, first := acquireInstance()
	if !first {
		return
	}

	workDir := core.WorkDir()
	if err := os.MkdirAll(workDir, 0700); err != nil {
		fatalBox("sbgui", "Cannot create the work directory:\n"+err.Error())
		return
	}
	store, err := core.OpenStore(workDir)
	if err != nil {
		fatalBox("sbgui", "Cannot open the node store:\n"+err.Error())
		return
	}
	mgr := core.NewManager(workDir)
	srv := server.New(store, mgr, workDir)

	// Start the web back-end first; if the port is taken, something
	// (very likely another sbgui) is already serving it -- say so and
	// stop instead of running a second sing-box owner.
	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fatalBox("sbgui", fmt.Sprintf(
			"Cannot start the web back-end on %s:\n%v\n\nAnother sbgui instance may already be running.", addr, err))
		return
	}
	go func() { _ = http.Serve(ln, srv.Handler()) }()

	a := newApp(store, mgr, srv, workDir, *port)
	if err := a.buildWindow(); err != nil {
		fatalBox("sbgui", "Cannot create the main window:\n"+err.Error())
		return
	}
	a.initTray()
	a.startTicker()

	// A later launch asks us (through the named event) to surface.
	if showEvt != 0 {
		go func() {
			for waitShow(showEvt) {
				a.mw.Synchronize(func() { a.showWindow() })
			}
		}()
	}

	// Same auto-start behaviour as the server build: if enabled and
	// nodes exist, bring the proxy up right away (in the background,
	// so a slow rule-set download never delays the window).
	if store.Data.Cfg.AutoStart && len(store.Data.Nodes) > 0 {
		go func() {
			if err := srv.StartProxy(); err != nil {
				a.postFail("Auto-start failed: %v", err)
			} else {
				a.postLog("Proxy auto-started")
			}
		}()
	}

	a.mw.Run()
	a.shutdown()
}
