SHELL := /bin/sh

GO ?= go
NPM ?= npm
WAILS ?= wails3
FRONTEND_DIR ?= frontend
WINDOWS_GOOS ?= windows
WINDOWS_GOARCH ?= amd64

.DEFAULT_GOAL := help

.PHONY: help fmt fmt-check tidy bindings frontend-install frontend-test frontend-build \
	test test-race vet check run build-windows clean

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

frontend-install: ## 安装前端依赖
	$(NPM) install --prefix $(FRONTEND_DIR)

frontend-test: frontend-install ## 执行前端测试
	$(NPM) run test --prefix $(FRONTEND_DIR)

frontend-build: frontend-install ## 构建前端静态资源
	$(NPM) run build --prefix $(FRONTEND_DIR)

test: ## 执行 Go 和前端测试
	$(GO) test ./...
	$(MAKE) frontend-test

test-race: ## 执行 Go race 检测
	$(GO) test -race ./...

vet: ## 执行 Go 静态检查（Linux 与 Windows 目标）
	$(GO) vet ./...
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 $(GO) vet ./...

check: fmt-check test-race vet frontend-test frontend-build ## 执行完整质量检查
	$(NPM) audit --audit-level=high --prefix $(FRONTEND_DIR)

run: ## 启动 Wails 开发模式
	$(WAILS) dev

build-windows: frontend-build ## 使用已有前端产物构建 Windows x64 版本
	@mkdir -p build/bin
	GOOS=$(WINDOWS_GOOS) GOARCH=$(WINDOWS_GOARCH) CGO_ENABLED=0 $(GO) build \
		-tags production -trimpath -buildvcs=false -ldflags="-w -s -H windowsgui" \
		-o build/bin/floating-translator.exe .

clean: ## 清理本地构建产物
	rm -rf build/bin frontend/dist/assets frontend/dist/index.html
