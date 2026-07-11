# 悬浮翻译器

悬浮翻译器是一个轻量级桌面翻译工具。复制英文文本后，程序会自动过滤无效或敏感内容，通过 OpenAI-compatible 大模型翻译为简体中文，并在主屏幕下方显示透明悬浮字幕。当前发布同时支持 Windows 10/11 x64 和 macOS 12+ Apple Silicon。

## 功能

- Windows 使用 `WM_CLIPBOARDUPDATE`，macOS 使用 `NSPasteboard` 轮询监听纯文本剪贴板；
- Windows 默认使用 `Ctrl+Alt+T`，macOS 默认使用 `Command+Option+T`（⌘⌥T）读取当前选区并直接发给模型；可选强制兼容模式只在原剪贴板为纯文本且能安全快照时临时模拟复制，复杂格式或期间出现新的复制内容会取消操作并保留最新剪贴板；
- 过滤空白、连续重复、中文、URL、代码、敏感凭据和超长文本；
- 自动翻译前后文本相同时不显示字幕，划词翻译始终显示结果或明确错误；
- 使用 Eino 调用 OpenAI-compatible ChatModel；
- 新内容会取消旧请求，迟到结果不会覆盖最新字幕；
- 透明、无边框、置顶、无焦点、鼠标穿透的字幕窗口；
- 字幕自动淡入、停留和淡出，新字幕会重置动画；
- Windows 系统托盘和 macOS 菜单栏图标支持暂停、恢复、重新加载配置、打开日志和退出；
- 托盘菜单展示当前划词快捷键，并可持久化开启或关闭划词翻译；
- 托盘“设置…”提供完整图形化配置，保存时自动补齐当前版本字段并立即生效；
- 配置错误或网络异常不会终止应用。

## 运行要求

- Windows 10 或 Windows 11 x64，以及 Microsoft Edge WebView2 Evergreen Runtime；或 macOS 12+ Apple Silicon；
- macOS 首次打开应用时会主动请求“辅助功能”权限；如果系统没有自动打开设置页，请到“系统设置 → 隐私与安全性 → 辅助功能”中允许悬浮翻译器。剪贴板监听本身不需要该权限；
- Go 1.26.5 或更高版本（发布构建需使用包含最新安全修复的补丁版本）；
- Node.js 20.19 或更高版本；
- Wails v3 CLI `v3.0.0-alpha2.117`，开发模式、重新生成前端绑定和 macOS 打包时需要。

## 首次配置

程序首次运行时会创建（macOS 路径中的 `~` 表示当前用户主目录）：

```text
%APPDATA%\FloatingTranslator\config.yaml
%APPDATA%\FloatingTranslator\logs\app.log
~/Library/Application Support/FloatingTranslator/config.yaml
~/Library/Application Support/FloatingTranslator/logs/app.log
```

首次生成的配置没有模型名称和 API Key，因此程序会保留托盘图标但禁用剪切板监听。编辑配置后，在托盘菜单中选择“重新加载配置”即可，无需重启。

推荐通过环境变量提供 API Key：

```powershell
$env:LLM_API_KEY = "your-api-key"
$env:LLM_BASE_URL = "https://api.openai.com/v1"
```

环境变量 `LLM_API_KEY` 和 `LLM_BASE_URL` 的优先级分别高于 `config.yaml` 中的 `llm.api_key` 和 `llm.base_url`。不设置 `LLM_BASE_URL` 时使用 YAML 配置中的地址。可参考 [config.example.yaml](config.example.yaml) 完成其他参数配置。

`llm.temperature` 默认为 `null`，表示不向模型接口发送该参数。Codex、推理模型或限制采样参数的模型应保持 `null`；只有确认模型支持时才配置 `0` 到 `2` 的数值。

划词翻译和字幕底部距离可通过以下配置调整：

```yaml
selection:
  enable: true
  hotkey: "Ctrl+Alt+T"
  compatibility_mode: false

# macOS 可改为：hotkey: "Command+Option+T"

subtitle:
  bottom_offset_percent: 4
```

macOS 新安装默认使用 `Command+Option+T`；从旧版本升级时，配置中的旧默认 `Ctrl+Alt+T` 会自动迁移为 macOS 快捷键。

`bottom_offset_percent` 表示字幕窗口距离主屏幕工作区底部的高度百分比，可配置范围为 `0` 到 `50`。修改后通过托盘菜单重新加载配置即可生效。

也可以从托盘菜单选择“设置…”，在“字幕外观”中直接调整底部距离。设置窗口会展示当前版本的全部常用字段；点击“保存并应用”后，完整字段会写入 YAML，已有注释、未知字段和未修改的 API Key 会保留。

API Key 输入框不会回显已有密钥；未修改时留空表示保留，输入后清空并保存表示移除密钥，应用会提示重新配置。

## 开发

安装前端依赖：

```bash
cd frontend
npm install
```

在 Windows 环境启动开发模式：

```bash
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-alpha2.117
make run
```

macOS 原生构建需要 Xcode Command Line Tools、`hdiutil` 和 Wails v3 CLI：

```bash
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-alpha2.117
make run
```

修改 Go 服务公开方法后，重新生成前端绑定：

```bash
make bindings
```

运行检查：

```bash
make check
```

## 构建

在 Windows x64 本机或 WSL 中交叉构建便携版，普通构建不依赖 Wails CLI：

```bash
make build-windows
```

输出文件：

```text
build/bin/floating-translator.exe
```

在 macOS Apple Silicon 上构建 `.app`、`.app.zip` 和 `.dmg`：

```bash
make package-macos
```

输出文件：

```text
build/bin/floating-translator-macos-arm64.dmg
build/bin/floating-translator-macos-arm64.app.zip
```

GitHub Actions 会在 push、Pull Request 或手动触发后并行执行 Windows x64 和 macOS arm64 检查与构建。普通运行会将 Windows 的 exe/zip 和 macOS 的 dmg/app.zip 上传为 workflow artifact；打开 GitHub 仓库的 Actions，进入对应运行记录，在 Artifacts 中下载。

推送 `v*` tag（例如 `v1.0.0`）时，workflow 会自动创建或更新 GitHub Release，并附上：

- `floating-translator.exe`
- `floating-translator-windows-amd64-v1.0.0.zip`
- `floating-translator-macos-arm64.dmg`
- `floating-translator-macos-arm64.app.zip`

运行配置和日志位于 Windows 的 `%APPDATA%\FloatingTranslator\` 或 macOS 的 `~/Library/Application Support/FloatingTranslator/`，无需在应用同目录放置密钥或配置。

## 隐私与日志

- 默认不保存剪切板历史，也不记录完整原文；
- 日志默认只记录文本长度、文本哈希、过滤原因、请求耗时和模型名称；
- 常见疑似密钥、Token、密码或私钥的内容不会发送给模型；过滤规则采用保守识别，仍不应把凭据复制到剪贴板；
- 如果显式开启 `logging.include_source_text`，已知凭据仍会被脱敏，但日志可能包含普通剪切板文本，请谨慎使用。

## MVP 边界

当前 MVP 只支持英文到简体中文、主屏幕、OpenAI-compatible 接口、Windows x64 便携版和 macOS arm64 DMG。macOS 首次启动会请求辅助功能权限。暂不包含安装器自动更新、翻译历史、OCR、多语言、活动屏幕跟随和专用 Ollama 适配器。
