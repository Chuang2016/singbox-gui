# sbgui — 轻量级 sing-box 图形管理工具

单文件 Go 程序 + 内嵌 Web 仪表盘，管理 sing-box 核心：导入链接/订阅、规则分流、实时流量、连接管理。

![platform](https://img.shields.io/badge/平台-Windows%20%7C%20macOS-blue) ![sing-box](https://img.shields.io/badge/sing--box-%3E%3D1.10-green)

## 功能

- **导入节点**：粘贴 `ss://` `vmess://` `vless://` `trojan://` `hysteria2://` `tuic://` 链接，每行一个
- **订阅**：支持 base64/v2rayN、sing-box JSON、Clash YAML 三种格式，可定时自动更新
- **规则分流**：三种预设（绕过大陆 / 全局代理 / 全局直连）+ 自定义域名/IP 规则
- **流量管理**：实时上下行曲线、活动连接列表、可一键断开连接
- **节点切换**：点击即切换出口，另有 `auto` 组按延迟自动优选
- **系统代理**：一键开关（Windows/macOS）

## 快速开始

1. 从 [sing-box releases](https://github.com/SagerNet/sing-box/releases) 下载对应平台的 sing-box（≥ 1.10，建议与本仓库同期的 1.14.x），解压得到 `sing-box`（Windows 为 `sing-box.exe`）
2. 把 `sbgui` 和 `sing-box` 放到**同一目录**
3. 运行 `sbgui`，浏览器打开 http://127.0.0.1:20123
4. 导入链接或添加订阅 → 点「启动」→ 点「开启系统代理」

预编译包见 [Releases](../../releases)（`sbgui.exe` / `sbgui-darwin-arm64` / `sbgui-darwin-amd64`）。

macOS 首次运行若提示“无法验证开发者”：`xattr -d com.apple.quarantine sbgui-darwin-arm64`

## 自己编译

```bash
./build.sh   # 输出 dist/: windows-amd64 / darwin-arm64 / darwin-amd64
```

纯 Go、无 CGO，Linux 下可直接交叉编译。

## 工作原理

- `sbgui` 根据你的节点/订阅/规则生成 `config.json`，用 `sing-box check` 校验后拉起 `sing-box run` 子进程
- 仪表盘经由 sing-box 的 Clash API 取实时流量（`/traffic`）与连接（`/connections`）
- 配置/节点/订阅存在用户目录（`~/.config/sbgui` / `%APPDATA%/sbgui`）

## iOS 说明

iOS App 无法从源码直接构建分发——需要 Mac + Xcode + Apple 付费开发者账号签名上架。
本仓库的 `core/`（链接解析、订阅抓取、配置生成）是纯逻辑包，可被 gomobile/Flutter 等方案复用；
日常在 iPhone 上使用，建议用 App Store 现有客户端（Shadowrocket / Stash / Surge / Loon 等），它们同样支持订阅与规则分流。

## 许可证

MIT
