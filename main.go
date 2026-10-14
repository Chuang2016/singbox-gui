// sbgui: 轻量级 sing-box 图形管理工具.
// 单二进制: 内嵌 Web 仪表盘, 管理 sing-box 子进程(配置文件自动生成),
// 支持链接/订阅导入、规则分流、实时流量与连接管理.
//
// 用法: ./sbgui [-port 20123]
// 首次运行请把对应平台的 sing-box 可执行文件放到 sbgui 同目录
// (https://github.com/SagerNet/sing-box/releases, 需要 >= 1.10).
package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/Chuang2016/singbox-gui/core"
	"github.com/Chuang2016/singbox-gui/server"
)

func main() {
	port := flag.Int("port", 20123, "dashboard port")
	flag.Parse()

	workDir := core.WorkDir()
	if err := os.MkdirAll(workDir, 0700); err != nil {
		fmt.Fprintln(os.Stderr, "workdir:", err)
		os.Exit(1)
	}
	store, err := core.OpenStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, "store:", err)
		os.Exit(1)
	}
	mgr := core.NewManager(workDir)
	srv := server.New(store, mgr, workDir)

	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	fmt.Printf("sbgui dashboard: http://%s\n", addr)
	fmt.Printf("work dir: %s\n", workDir)
	fmt.Println("把 sing-box 可执行文件放到 work dir(或 PATH)后, 点页面上的 启动 即可.")

	go func() {
		if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
			fmt.Fprintln(os.Stderr, "http:", err)
			os.Exit(1)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	_ = mgr.Stop()
	fmt.Println("\nbye")
}
