# ClipboardFileServer

一个轻量的局域网剪贴板和文件服务器。浏览器打开后，可以粘贴文本、截图图片或文件，服务端会自动保存为历史条目；其他机器访问同一个地址即可复制文本或下载文件。

服务端是无第三方依赖的 Go 程序，前端文件会被内嵌进二进制。分发时只需要一个 `clip-server` 可执行文件，不需要 Node.js、npm 或 clone 仓库。

下载页：<https://guajun.github.io/ClipboardFileServer/>

## 使用

如果已经拿到对应平台的二进制：

```sh
clip-server --host 0.0.0.0 --port 8787
```

服务启动后会打印本机可访问的 URL。浏览器访问其中一个地址即可使用。

也可以从源码直接运行，构建机需要 Go 1.21 或更高版本：

```sh
go run . --host 0.0.0.0 --port 8787
```

PowerShell、bash、Termux bash 的启动参数相同。也可以用环境变量配置：

```sh
PORT=8787 HOST=0.0.0.0 clip-server
```

PowerShell：

```powershell
$env:PORT = "8787"
$env:HOST = "0.0.0.0"
clip-server
```

## 可选安全令牌

默认适合可信局域网使用。若需要简单保护，可以设置令牌：

```sh
CLIP_TOKEN=change-me clip-server --host 0.0.0.0
```

PowerShell：

```powershell
$env:CLIP_TOKEN = "change-me"
clip-server --host 0.0.0.0
```

访问页面时把令牌放在 URL 中一次即可：

```text
http://服务器IP:8787/?token=change-me
```

前端会把令牌保存在当前浏览器的 localStorage 中，并在 API 请求和下载链接中携带。

## 浏览器使用

- 在输入区粘贴文本、截图或文件，会自动创建历史条目。
- 也可以输入文本后点“保存文本”。
- 文件可拖入输入区，或通过“选择文件”上传。
- 文本条目可以复制回当前设备剪贴板。

## 命令行上传

上传文本：

```sh
printf 'hello from shell' | curl -X POST --data-binary @- http://127.0.0.1:8787/api/text
```

上传文件：

```sh
curl -X POST --data-binary @screenshot.png "http://127.0.0.1:8787/api/upload?name=screenshot.png"
```

如果启用了 `CLIP_TOKEN`，加请求头：

```sh
curl -H "X-Clip-Token: change-me" -X POST --data-binary @screenshot.png "http://127.0.0.1:8787/api/upload?name=screenshot.png"
```

## 数据目录

默认数据保存在当前工作目录的 `data/`，其中 `entries.json` 存历史索引，`files/` 存上传文件。可以改到其他位置：

```sh
clip-server --data-dir /path/to/clipboard-data
```

## 构建二进制

构建当前平台：

```sh
go build -trimpath -o dist/clip-server .
```

Windows 当前平台：

```powershell
go build -trimpath -o dist\clip-server.exe .
```

一次构建常见平台：

```sh
sh scripts/build-release.sh 0.1.0
```

PowerShell：

```powershell
.\scripts\build-release.ps1 -Version 0.1.0
```

输出文件在 `dist/`：

```text
index.html
install.sh
install.ps1
clip-server_linux_amd64
clip-server_linux_arm64
clip-server_darwin_amd64
clip-server_darwin_arm64
clip-server_windows_amd64.exe
clip-server_windows_arm64.exe
```

其中 `index.html` 是静态下载页，可以和二进制、安装脚本一起直接上传到你的 HTTP 服务器目录。

仓库 push 到 `master` 后，GitHub Actions 会自动构建 `dist/` 并部署到 GitHub Pages：

```text
https://guajun.github.io/ClipboardFileServer/
```

## 一行安装

推荐直接使用项目下载页：

```text
https://guajun.github.io/ClipboardFileServer/
```

Linux、Termux、macOS：

```sh
curl -fsSL https://guajun.github.io/ClipboardFileServer/install.sh | sh
```

Windows PowerShell：

```powershell
& ([scriptblock]::Create((Invoke-RestMethod 'https://guajun.github.io/ClipboardFileServer/install.ps1')))
```

安装脚本会自动识别系统和架构。当前支持：

- Windows amd64 / arm64
- Linux amd64 / arm64
- Termux arm64
- macOS amd64 / arm64

如果你有自己的 HTTP 服务器，也可以把下面文件放到同一个目录，例如 `https://your-server/clipboard/`：

- `dist/index.html`
- `dist/install.sh`
- `dist/install.ps1`
- `dist/` 里的目标平台二进制

然后用自托管地址安装：

```sh
curl -fsSL https://your-server/clipboard/install.sh | sh -s -- https://your-server/clipboard/
```

PowerShell：

```powershell
& ([scriptblock]::Create((Invoke-RestMethod 'https://your-server/clipboard/install.ps1'))) -BaseUrl 'https://your-server/clipboard/'
```

也可以手动指定某个二进制 URL：

```sh
curl -fsSL https://guajun.github.io/ClipboardFileServer/install.sh | sh -s -- https://guajun.github.io/ClipboardFileServer/clip-server_linux_amd64
```

PowerShell：

```powershell
& ([scriptblock]::Create((Invoke-RestMethod 'https://guajun.github.io/ClipboardFileServer/install.ps1'))) -BinaryUrl 'https://guajun.github.io/ClipboardFileServer/clip-server_windows_amd64.exe'
```

安装完成后，所有平台都用同一个启动命令：

```sh
clip-server --host 0.0.0.0 --port 8787
```

以上一行安装命令会直接执行服务器返回的脚本；只建议对你自己控制的服务器这样做。更稳妥的做法是先下载脚本看一眼，再运行。

## 检查

```sh
go test ./...
```