# Ros

Windows-only `Go + Walk` 图形程序，用于通过 SSH 隧道自动拉起 `mstsc` 连接远程桌面。

## 功能

- 程序启动即显示服务器列表，单击服务器开始连接流程。
- 每台服务器支持配置多条 SSH 隧道，按优先级顺序依次尝试。
- SSH 隧道支持：
  - 直连
  - HTTP CONNECT 代理
  - SOCKS5 代理
- 隧道建立后自动：
  - 分配本地随机端口映射到远端 `3389`（或配置端口）
  - 使用 `cmdkey` 写入临时凭据
  - 在临时目录生成 `.rdp` 文件
  - 启动 `mstsc` 并自动登录
- 会话结束后自动清理：
  - 关闭隧道
  - 删除临时 `.rdp`
  - 删除临时 `cmdkey` 凭据
- RDP 可配置项：
  - 分辨率自适应 / 固定分辨率
  - 磁盘重定向
  - 声音重定向
  - 剪切板同步

## 配置文件

- 路径：`%APPDATA%\Ros\config.json`

## 本地构建

```powershell
rsrc -manifest app.manifest -ico app.ico -o rsrc.syso
go mod tidy
go build -o Ros.exe .
```

> `rsrc.syso` 用于嵌入 `Common-Controls v6` manifest，确保原版 Walk 在部分 Windows 环境不触发 tooltip 初始化错误。

## 运行

```powershell
.\Ros.exe
```
