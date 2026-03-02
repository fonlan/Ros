# Ros

Windows 平台的 `RDP over SSH` 图形工具，基于 `Go + Walk`。  
用于通过 SSH 隧道映射远端 RDP 端口，并自动拉起 `mstsc` 完成登录。

## 功能亮点

- Windows-only 本地 GUI，启动后直接显示服务器列表。
- 单击服务器即发起连接（按隧道优先级依次尝试，直到成功）。
- 会话中 SSH 隧道若断连，会自动重连；重连失败时会继续尝试后续优先级隧道。
- SSH 隧道支持：
  - 直连
  - HTTP CONNECT 代理
  - SOCKS5 代理
  - 可选 SSH 跳板机（二跳建链）
  - 配置跳板机时，代理仅用于连接跳板机
- 连接流程自动化：
  - 本地随机端口转发到远端 RDP 端口（默认 `3389`）
  - 使用 `cmdkey` 写入临时凭据
  - 生成临时 `.rdp` 文件并启动 `mstsc`
- 会话结束自动清理：
  - 关闭隧道
  - 删除临时 `.rdp`
  - 删除临时 `cmdkey` 凭据
- 支持系统托盘最小化/恢复。
- RDP 参数可配置：
  - 自适应或固定分辨率
  - 磁盘重定向
  - 声音重定向
  - 剪切板同步

## 工作流程

1. 选择服务器。
2. 按优先级尝试 SSH 隧道（可选跳板机；若配置跳板机则代理仅作用于跳板机）。
3. 会话期间如隧道断连，自动重连；失败则切换后续优先级隧道。
4. 隧道成功后写入临时 RDP 凭据并生成 `.rdp` 文件。
5. 调起 `mstsc` 连接 `127.0.0.1:<随机端口>`。
6. 远程会话结束后自动清理所有临时资源。

## 环境要求

- Windows 10/11
- Go `1.22+`
- `rsrc`（用于嵌入 manifest 与图标）

## 快速开始

### 1) 安装依赖工具（仅首次）

```powershell
go install github.com/akavel/rsrc@latest
```

确保 `rsrc` 在 `PATH` 中可执行。

### 2) 生成资源并构建

```powershell
rsrc -manifest app.manifest -ico app.ico -o rsrc.syso
go mod tidy
go build -trimpath -ldflags "-H=windowsgui -s -w -buildid=" -o Ros.exe .
```

> `rsrc.syso` 会嵌入 `Common-Controls v6` manifest，避免部分环境下 Walk 的 tooltip 初始化异常。

### 3) 运行

```powershell
.\Ros.exe
```

## 使用说明

1. 点击右上角 `+` 新增服务器。
2. 在服务器配置中新增至少 1 条 SSH 隧道并设置优先级。
3. 配置 RDP 用户名、密码和显示/重定向选项。
4. 在主窗口单击服务器条目开始连接。
5. 连接成功后窗口会自动最小化到托盘，结束远程会话后自动恢复。

## 配置文件

- 默认路径：`%APPDATA%\Ros\config.json`

## 项目结构

核心文件：

- `main_windows.go`: 程序入口。
- `ui_main_windows.go`: 主窗口、托盘逻辑、连接流程触发。
- `ui_server_dialog_windows.go`: 服务器配置对话框。
- `ui_tunnel_dialog_windows.go`: SSH 隧道配置对话框。
- `tunnel_windows.go`: SSH 建链与端口转发（含代理支持）。
- `rdp_windows.go`: `.rdp` 生成、`cmdkey` 凭据管理、`mstsc` 启动与清理。
- `config_windows.go`: 配置结构与读写。

## 安全说明

- 当前配置文件中的密码字段为明文存储（`config.json`）。
- 建议仅在可信主机使用，配合系统级磁盘加密或用户权限隔离。

## 已知限制

- 仅支持 Windows。
- SSH 主机指纹校验当前使用 `InsecureIgnoreHostKey`（适合内网/受控环境，生产环境请按需强化）。
