# 悬浮翻译器

复制英文，自动翻译成简体中文，并在屏幕下方显示透明悬浮字幕。

悬浮翻译器是一个基于 Go、Vue 3 和 Wails v3 的桌面应用，支持 OpenAI-compatible 接口。它同时提供剪贴板监听和选区翻译两种方式，优先保持原生窗口体验，并尽量避免干扰用户现有的剪贴板内容。

当前发布目标：

- Windows 10/11 x64 便携版；
- macOS 12+ Apple Silicon（arm64）DMG；
- 默认英文 → 简体中文；
- 主屏幕透明、无边框、置顶字幕窗口。

## 下载

稳定版本请前往 [GitHub Releases](https://github.com/betterlmy/Floating-Translator/releases) 下载。

| 平台 | 文件 | 说明 |
| --- | --- | --- |
| Windows x64 | `floating-translator.exe` 或 `.zip` | 便携版，需要 WebView2 Evergreen Runtime |
| macOS Apple Silicon | `.dmg` | 打开后将应用拖入 Applications |
| macOS Apple Silicon | `.app.zip` | 适合脚本或手动解压安装 |

每次 push、Pull Request 或手动运行 GitHub Actions 后，构建产物也会作为 Artifact 提供下载：打开仓库的 **Actions**，进入对应运行记录，在页面底部的 **Artifacts** 中下载。

推送 `v*` 格式的 tag（例如 `v1.0.0`）后，CI 会自动创建或更新 GitHub Release，并上传 Windows 和 macOS 的正式构建产物。

## 功能

- 监听剪贴板中的纯文本，经过防抖、去重和安全过滤后自动翻译；
- 过滤空文本、重复文本、URL、代码、超长内容和疑似密钥/Token/密码；
- 使用全局快捷键读取当前选区并翻译；
- 新请求会取消旧请求，迟到的模型结果不会覆盖最新字幕；
- 字幕窗口支持宽度、位置、字号、背景透明度和淡入/停留/淡出时长配置；
- Windows 系统托盘和 macOS 菜单栏提供暂停、恢复、重新加载配置、设置、打开日志和退出；
- 设置页修改后立即生效，并保留 YAML 中的未知字段、注释和未修改的 API Key；
- API Key 不会回显到设置页，也不会默认写入日志。

## 安装与首次启动

### Windows

1. 下载并解压 Windows x64 ZIP，或直接运行 `floating-translator.exe`。
2. 确认已安装 [Microsoft Edge WebView2 Evergreen Runtime](https://developer.microsoft.com/microsoft-edge/webview2/)。
3. 第一次启动后，编辑生成的配置文件，或从托盘菜单打开“设置…”。
4. 配置模型和 API Key 后，在托盘菜单中选择“重新加载配置”。

这是便携版，不会自动写入安装目录之外的程序文件；运行配置和日志位于用户目录中。

### macOS

1. 打开 DMG，将“悬浮翻译器”拖入 `Applications`。
2. 第一次启动时允许“辅助功能”权限。该权限用于读取选区、注册全局快捷键和兼容复制；剪贴板监听本身不需要该权限。
3. 如果系统没有自动打开权限页面，进入“系统设置 → 隐私与安全性 → 辅助功能”，手动允许悬浮翻译器。
4. 在菜单栏图标中打开“设置…”，配置模型后保存。

当前 macOS 包使用 ad-hoc 签名，未配置 Apple Developer notarization。如果系统提示无法验证开发者，可在 Finder 中右键应用选择“打开”，或在“隐私与安全性”中允许本次启动。

## 配置

首次运行会生成配置文件和日志目录：

| 平台 | 配置文件 | 日志目录 |
| --- | --- | --- |
| Windows | `%APPDATA%\FloatingTranslator\config.yaml` | `%APPDATA%\FloatingTranslator\logs\` |
| macOS | `~/Library/Application Support/FloatingTranslator/config.yaml` | `~/Library/Application Support/FloatingTranslator/logs/` |

完整示例见 [config.example.yaml](config.example.yaml)。最少需要填写模型名称和 API Key：

```yaml
llm:
  provider: "openai_compatible"
  base_url: "https://api.openai.com/v1"
  api_key: "${LLM_API_KEY}"
  model: "your-model-name"
  temperature: null
  timeout_seconds: 20
```

也可以使用环境变量覆盖 YAML 中的对应值：

```bash
export LLM_API_KEY="your-api-key"
export LLM_BASE_URL="https://api.openai.com/v1"
```

Windows PowerShell：

```powershell
$env:LLM_API_KEY = "your-api-key"
$env:LLM_BASE_URL = "https://api.openai.com/v1"
```

`LLM_API_KEY` 和 `LLM_BASE_URL` 的优先级高于 YAML 配置。通过 Finder 或开始菜单启动应用时，Shell 中临时设置的环境变量不一定会被继承；需要时请使用系统环境变量，或直接在配置文件中填写。

### API Key 行为

- 设置页只返回“已配置”状态，不会返回已有 API Key 明文；
- API Key 输入框留空且未标记修改时，保存会保留原 Key；
- 输入新 Key 后再清空并保存，会明确移除原 Key；
- 移除 Key 后应用会进入配置错误状态，重新填写 Key 并保存即可恢复。

`llm.temperature` 默认为 `null`，表示不向接口发送该参数。推理模型或不支持采样温度的模型建议保持 `null`；只有确认服务支持时才填写 `0` 到 `2` 的数值。

## 使用方式

### 剪贴板翻译

复制文本后，应用会自动处理最新剪贴板内容。默认只接受英文信号明显的文本；开启“仅翻译英文”时，会使用 ASCII 英文字母比例和英文词汇信号判断，并拒绝带重音的其他拉丁文字。URL、代码、敏感内容和超长文本默认跳过。

可在设置页的“剪贴板”中调整：

- 是否监听剪贴板；
- 防抖时间和最大文本长度；
- 是否跳过 URL、代码、敏感内容；
- 英文最低比例和中文最高比例。

### 选区翻译

选中文本后按全局快捷键：

| 平台 | 默认快捷键 |
| --- | --- |
| Windows | `Ctrl+Alt+T` |
| macOS | `Command+Option+T`（⌘⌥T） |

macOS 会把旧配置中的 `Ctrl+Alt+T` 自动迁移为 `Command+Option+T`。快捷键支持 Ctrl/Command、Alt/Option、Shift 加字母、数字或 F1-F24，可在设置页修改。

默认情况下，应用优先通过 Windows UI Automation 或 macOS Accessibility 直接读取选区。只有打开“强制兼容”后，直接读取失败才会临时模拟复制：

- 原剪贴板包含复杂格式时直接终止，不覆盖原内容；
- 快照会保存所有允许的纯文本表示并逐一恢复；
- 复制期间检测到用户或其他程序产生新的剪贴板内容时取消恢复，并保留最新内容；
- 选区读取期间到达的剪贴板文本会在选区流程结束后继续处理。

### 字幕窗口

字幕默认显示在主屏幕下方。可在“字幕外观”中调整宽度、底部距离、字体、字号、最大行数、背景透明度和动画时长。`bottom_offset_percent` 的有效范围为 `0` 到 `50`。

## 隐私与安全

- 应用不保存剪贴板历史；
- 默认日志只记录文本长度、哈希、过滤原因、模型名和耗时，不记录完整原文；
- 日志中的外部错误会统一脱敏并截断，避免暴露 URL 查询参数、Token 或认证信息；
- 只有通过过滤并进入翻译流程的文本才会发送到配置的模型接口；
- 开启 `logging.include_source_text` 后，日志可能包含普通剪贴板文本，仅建议在排查问题时临时启用；
- 兼容复制采用 fail-closed 策略，无法安全快照或恢复时宁可取消操作；
- 不要把真实凭据复制到剪贴板，也不要将真实 API Key 提交到仓库、配置示例或 Issue。

## 开发

### 环境要求

- Go 1.26.5 或更高版本；
- Node.js 20.19 或更高版本；
- Wails v3 CLI `v3.0.0-alpha2.117`；
- macOS 原生构建需要 Xcode Command Line Tools、`hdiutil` 和 Apple Silicon 主机。

安装前端依赖：

```bash
npm ci --prefix frontend
```

启动开发模式：

```bash
make run
```

修改 Go 服务公开方法后，重新生成 TypeScript 绑定：

```bash
make bindings
```

常用质量检查：

```bash
make fmt-check
go test ./...
go test -race ./...
make vet
make frontend-test
make frontend-build
make vulncheck
```

一次执行完整门禁：

```bash
make check
```

`make check` 会执行 Go race/vet、前端测试和构建、Windows 目标 vet 以及前端高危依赖审计。

## 本地构建

Windows x64 便携程序：

```bash
make build-windows
```

输出：

```text
build/bin/floating-translator.exe
```

macOS arm64 应用、ZIP 和 DMG：

```bash
make package-macos
```

输出：

```text
build/bin/Floating Translator.app
build/bin/floating-translator-macos-arm64.app.zip
build/bin/floating-translator-macos-arm64.dmg
```

`build/bin/`、`frontend/dist/` 和 `frontend/node_modules/` 都是本地构建产物，不应提交到 Git。

## CI 与发布

工作流位于 [.github/workflows/windows.yml](.github/workflows/windows.yml)，会在 push、Pull Request 和手动触发时执行：

- Windows x64 原生测试、vet、便携程序构建和漏洞扫描；
- macOS arm64 原生测试、DMG/ZIP 构建和漏洞扫描；
- macOS runner 上的 Windows 平台测试交叉编译检查。

只有推送 `v*` tag 时才会进入发布 job，自动创建或更新 GitHub Release，并上传：

- `floating-translator.exe`；
- `floating-translator-windows-amd64-<tag>.zip`；
- `floating-translator-macos-arm64.dmg`；
- `floating-translator-macos-arm64.app.zip`。

## 当前限制

- 目前只提供英文到简体中文翻译；
- 只定位主屏幕字幕，不跟随活动窗口或指定屏幕；
- macOS 当前只构建 Apple Silicon arm64；
- 不包含翻译历史、OCR、自动更新、安装器和专用 Ollama 适配器；
- 需要一个可用的 OpenAI-compatible 模型服务，应用本身不内置翻译模型。
