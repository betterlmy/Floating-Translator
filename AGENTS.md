# AGENTS.md

## 项目概览

Floating Translator 是一个 Windows 10/11 x64 优先的 Wails v3 桌面应用。它监听剪贴板或通过热键读取选中文本，经 OpenAI-compatible 模型翻译后，以无边框、透明、置顶的字幕窗口显示结果。

- Go 后端位于仓库根目录和 `internal/`，负责应用生命周期、配置、平台能力、文本过滤、翻译与调度。
- Vue 3 + TypeScript 前端位于 `frontend/`，负责字幕展示与设置界面。
- Wails 将 Go 服务方法生成到 `frontend/bindings/`，前端通过 `frontend/src/runtime_bridge.ts` 调用。

## 目录约定

- `app.go`：应用服务、窗口控制与前后端事件。
- `main.go`、`app_wails_windows.go`：Windows/Wails 启动与窗口配置；`main_stub.go` 支持非 Windows 校验。
- `internal/config/`：YAML 配置、默认值和安全写回。
- `internal/filter/`：文本规范化、去重与敏感内容过滤。
- `internal/processor/`：取消旧请求、保证最新请求优先的翻译调度。
- `internal/platform/`、`internal/hotkey/`：Windows 桌面、剪贴板和热键集成。
- `internal/translator/`：Eino/OpenAI-compatible 翻译实现。
- `frontend/src/`：Vue 组件、样式、运行时桥接和类型。
- `frontend/bindings/`：Wails 生成代码。除重新生成绑定外不要手动编辑。
- `build/`：图标、平台元数据和打包资源；`build/bin/` 为可删除的本地构建产物。

## 开发原则

- 保持 MVP 的边界：默认英文翻译为简体中文、主屏字幕窗口、Windows x64。
- 不要记录、提交或输出 API Key、剪贴板原文或其他敏感信息。日志默认只应包含长度、哈希、过滤原因、耗时和模型名。
- 处理异步翻译时必须保留 `processor.Processor` 的取消和序列号语义：旧请求及迟到结果不得覆盖最新结果。
- 修改配置时保留未知 YAML 字段、注释和未变更的 API Key；优先复用 `internal/config` 的读写路径。
- Windows 专属能力应放在带 `*_windows.go` 的文件中，并为非 Windows 的构建和测试保留可编译的 stub。
- 对外暴露或修改 Go 服务方法后，更新前端调用与类型，并运行 `make bindings` 重新生成 `frontend/bindings/`。

## 代码风格

- Go：提交前运行 `gofmt`；使用标准库错误包装（`fmt.Errorf("…: %w", err)`）；将实现与同包 `*_test.go` 测试一同维护。
- TypeScript/Vue：使用 `<script setup lang="ts">`、显式类型和现有的两空格缩进风格；通过 `runtime_bridge.ts` 隔离 Wails runtime 与生成绑定。
- 保持用户可见中文文案和现有文件编码不被意外改写。
- 不要手动改 `go.sum`、`frontend/package-lock.json` 或生成绑定，除非相应依赖或生成结果确实发生变化。

## 常用命令

在仓库根目录运行：

```bash
# Go 格式化与检查
make fmt
make fmt-check
go test ./...
go test -race ./...
make vet

# 前端（首次或依赖变更后会安装依赖）
make frontend-test
make frontend-build

# 完整质量检查：格式、Go 测试/race/vet、前端测试/构建及高危依赖审计
make check

# 需要 Windows、WebView2 与 Wails v3 CLI 的本地开发
make run

# 重新生成 TypeScript 绑定（Go 服务 API 变更后）
make bindings

# 生成 Windows x64 便携程序
make build-windows
```

`make check` 会执行 `npm audit --audit-level=high`，因此可能需要网络访问。仅修改 Go 或前端局部代码时，至少运行对应测试；涉及调度、配置、平台代码或发布构建时运行 `make check`。

## 配置与本地文件

- 运行时配置和日志位于 `%APPDATA%\\FloatingTranslator\\`，不在仓库中。
- 使用 `config.example.yaml` 作为配置示例；不要新增真实凭据到 `config.yaml`、`.env`、测试夹具或文档。
- `LLM_API_KEY` 和 `LLM_BASE_URL` 可覆盖 YAML 中的对应配置。测试应使用假实现或 mock，不能调用真实模型服务。

## 提交前检查

- 确认 `git status --short` 只包含预期文件；不得带入 `frontend/node_modules/`、`frontend/dist/`、`build/bin/`、日志、配置或凭据。
- 为行为变更添加或更新最贴近实现位置的测试。
- 如果改动了界面、窗口行为或前后端事件，确认事件名、负载字段和 `frontend/src/settings_types.ts` / `runtime_bridge.ts` 保持一致。
