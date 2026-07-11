SHELL := /bin/sh

GO ?= go
NPM ?= npm
WAILS ?= wails3
GOVULNCHECK ?= govulncheck
FRONTEND_DIR ?= frontend
WINDOWS_GOOS ?= windows
WINDOWS_GOARCH ?= amd64
MACOS_GOOS ?= darwin
MACOS_GOARCH ?= arm64
MACOS_DEPLOYMENT_TARGET ?= 12.0
MACOS_APP_DIR ?= build/bin/Floating Translator.app
MACOS_BINARY ?= build/bin/floating-translator
MACOS_ICON ?= build/bin/floating-translator.icns
MACOS_DMG ?= build/bin/floating-translator-macos-$(MACOS_GOARCH).dmg
MACOS_APP_ZIP ?= build/bin/floating-translator-macos-$(MACOS_GOARCH).app.zip
MACOS_VERSION ?= 0.1.0

.DEFAULT_GOAL := help


.PHONY: help fmt fmt-check tidy bindings syso frontend-install frontend-test frontend-build \
	test test-race vet vulncheck check run build-windows macos-icon build-macos \
	package-macos clean

help: ## 显示可用目标
	@awk 'BEGIN {FS = ":.*##"; printf "用法: make <目标>\n\n目标:\n"} /^[a-zA-Z0-9_-]+:.*##/ {printf "  %-24s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

fmt: ## 格式化 Go 代码
	$(GO) fmt ./...

fmt-check: ## 检查 Go 代码是否已经格式化
	@test -z "$$(gofmt -l $$(rg --files -g '*.go'))" || (echo '以下 Go 文件需要 gofmt:'; gofmt -l $$(rg --files -g '*.go'); exit 1)

tidy: ## 整理 Go 模块依赖
	$(GO) mod tidy

bindings: ## 使用 Wails v3 CLI 重新生成前端绑定
	@command -v $(WAILS) >/dev/null 2>&1 || (echo '未找到 $(WAILS)，请先安装 Wails v3 CLI'; exit 1)
	$(WAILS) generate bindings -ts -d frontend/bindings

syso: ## 生成 Windows 可执行文件图标和版本资源
	@command -v $(WAILS) >/dev/null 2>&1 || (echo '未找到 $(WAILS)，请先安装 Wails v3 CLI'; exit 1)
	$(WAILS) generate syso -manifest build/windows/wails.exe.manifest -info build/windows/info.json -icon build/windows/icon.ico -out rsrc_windows_amd64.syso -arch amd64

frontend-install: ## 安装前端依赖
	$(NPM) ci --prefix $(FRONTEND_DIR)

frontend-test: frontend-install ## 执行前端测试
	$(NPM) run test --prefix $(FRONTEND_DIR)

frontend-build: frontend-install ## 构建前端静态资源
	$(NPM) run build --prefix $(FRONTEND_DIR)

test: ## 执行 Go 和前端测试
	$(GO) test ./...
	$(MAKE) frontend-test

test-race: ## 执行 Go race 检测
	$(GO) test -race ./...

vet: frontend-build ## 执行 Go 静态检查（Linux 与 Windows 目标）
	$(GO) vet ./...
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 $(GO) vet ./...

vulncheck: ## 执行 Go 可达漏洞扫描（发布门禁）
	@command -v $(GOVULNCHECK) >/dev/null 2>&1 || (echo '未找到 $(GOVULNCHECK)，请先安装 govulncheck'; exit 1)
	$(GOVULNCHECK) ./...

check: fmt-check test-race vet frontend-test frontend-build ## 执行完整质量检查
	$(NPM) audit --audit-level=high --prefix $(FRONTEND_DIR)

run: ## 启动 Wails 开发模式
	$(WAILS) dev

build-windows: frontend-build syso ## 使用已有前端产物构建 Windows x64 版本
	@mkdir -p build/bin
	GOOS=$(WINDOWS_GOOS) GOARCH=$(WINDOWS_GOARCH) CGO_ENABLED=0 $(GO) build \
		-tags production -trimpath -buildvcs=false -ldflags="-w -s -H windowsgui" \
		-o build/bin/floating-translator.exe .

macos-icon: ## 生成 macOS 应用图标
	@command -v $(WAILS) >/dev/null 2>&1 || (echo '未找到 $(WAILS)，请先安装 Wails v3 CLI'; exit 1)
	@mkdir -p build/bin
	$(WAILS) generate icons -input build/appicon.png \
		-macfilename $(MACOS_ICON) -windowsfilename build/bin/floating-translator.ico

build-macos: frontend-build macos-icon ## 构建 macOS arm64 .app
	@mkdir -p build/bin
	GOOS=$(MACOS_GOOS) GOARCH=$(MACOS_GOARCH) CGO_ENABLED=1 \
		CGO_CFLAGS="-mmacosx-version-min=$(MACOS_DEPLOYMENT_TARGET)" \
		CGO_LDFLAGS="-mmacosx-version-min=$(MACOS_DEPLOYMENT_TARGET)" \
		MACOSX_DEPLOYMENT_TARGET=$(MACOS_DEPLOYMENT_TARGET) $(GO) build \
		-tags production -trimpath -buildvcs=false -ldflags="-w -s" \
		-o "$(MACOS_BINARY)" .
	@rm -rf "$(MACOS_APP_DIR)"
	@mkdir -p "$(MACOS_APP_DIR)/Contents/MacOS" "$(MACOS_APP_DIR)/Contents/Resources"
	@cp "$(MACOS_BINARY)" "$(MACOS_APP_DIR)/Contents/MacOS/floating-translator"
	@cp "$(MACOS_ICON)" "$(MACOS_APP_DIR)/Contents/Resources/iconfile.icns"
	@sed "s/0.1.0/$(MACOS_VERSION)/g" build/darwin/Info.plist \
		> "$(MACOS_APP_DIR)/Contents/Info.plist"
	@chmod +x "$(MACOS_APP_DIR)/Contents/MacOS/floating-translator"
	@xattr -cr "$(MACOS_APP_DIR)" 2>/dev/null || true
	@codesign --force --deep --sign - "$(MACOS_APP_DIR)"

package-macos: build-macos ## 生成 macOS .app.zip 和 .dmg
	@rm -f "$(MACOS_APP_ZIP)" "$(MACOS_DMG)"
	@staging="$$(mktemp -d)"; \
		set -e; \
		trap 'rm -rf "$$staging"' EXIT; \
		ditto --norsrc "$(MACOS_APP_DIR)" "$$staging/Floating Translator.app"; \
		xattr -cr "$$staging/Floating Translator.app" 2>/dev/null || true; \
		codesign --force --deep --sign - "$$staging/Floating Translator.app"; \
		codesign --verify --deep --strict "$$staging/Floating Translator.app"; \
		ditto --norsrc -c -k --keepParent "$$staging/Floating Translator.app" "$(MACOS_APP_ZIP)"; \
		hdiutil create -volname "悬浮翻译器" -srcfolder "$$staging" \
			-ov -format UDZO "$(MACOS_DMG)"

clean: ## 清理本地构建产物
	rm -rf build/bin frontend/dist/assets frontend/dist/index.html
