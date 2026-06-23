.PHONY: run dev build agentred agentred-linux agentred-deploy agentred-deploy-restart agentred-deploy-local-coding agentred-local-coding generate test test-backend test-frontend test-cover lint lint-backend lint-frontend lint-fix lint-fix-backend lint-fix-frontend mock install install-deps clean check e2e e2e-scratch

APP_NAME := Agentre
VERSION ?= 0.1.0
ifeq ($(OS),Windows_NT)
NULLDEV := NUL
UNAME_S := Windows_NT
WAILS ?= wails
else
NULLDEV := /dev/null
UNAME_S := $(shell uname -s 2>$(NULLDEV) || echo unknown)
WAILS ?= $(shell command -v wails 2>$(NULLDEV) || printf "%s/bin/wails" "$$(go env GOPATH)")
endif
COMMIT_ID := $(shell git rev-parse --short HEAD 2>$(NULLDEV) || echo unknown)
VERSION_PKG := github.com/cago-frame/cago/configs
BUILDINFO_PKG := github.com/agentre-ai/agentre/internal/buildinfo
LDFLAGS := -s -w -X $(VERSION_PKG).Version=$(VERSION) -X $(BUILDINFO_PKG).CommitID=$(COMMIT_ID)
FRONTEND_DIR := frontend
BACKEND_PKGS := . ./cmd/... ./internal/... ./migrations ./pkg/...
E2E_SPEC ?=

MACOS_APP_INSTALL_DIR ?= /Applications
PREFIX ?= /usr/local
WAILS_PLATFORM ?=
WAILS_BUILD_FLAGS ?=
AGENTRED_BUILD_DIR ?= build/bin
AGENTRED_LOCAL_BINARY := $(AGENTRED_BUILD_DIR)/agentred
AGENTRED_GOOS ?= linux
AGENTRED_GOARCH ?= amd64
AGENTRED_LINUX_BINARY := $(AGENTRED_BUILD_DIR)/agentred-$(AGENTRED_GOOS)-$(AGENTRED_GOARCH)
AGENTRED_TARGET ?= local-coding
AGENTRED_REMOTE_PATH ?= /usr/local/bin/agentred
AGENTRED_REMOTE_TMP ?= /tmp/agentred.$(COMMIT_ID)
AGENTRED_RUN_ARGS ?= run
AGENTRED_LOG_PATH ?= /tmp/agentred.log
AGENTRED_RESTART_CMD ?= pkill -x agentred || true; sleep 1; nohup $(AGENTRED_REMOTE_PATH) $(AGENTRED_RUN_ARGS) >$(AGENTRED_LOG_PATH) 2>&1 </dev/null & sleep 1; $(AGENTRED_REMOTE_PATH) status >/dev/null

# 开发模式(前后端热重载)
dev:
	@mkdir -p $(FRONTEND_DIR)/dist && [ -e $(FRONTEND_DIR)/dist/.keep ] || touch $(FRONTEND_DIR)/dist/.keep
	"$(WAILS)" dev

# 构建生产版本(默认当前平台；可用 WAILS_PLATFORM 跨平台构建)
build:
	"$(WAILS)" build -ldflags="$(LDFLAGS)" $(if $(strip $(WAILS_PLATFORM)),-platform "$(WAILS_PLATFORM)") $(WAILS_BUILD_FLAGS)

# 构建 agentred(当前平台)
agentred:
	mkdir -p "$(AGENTRED_BUILD_DIR)"
	go build -ldflags="$(LDFLAGS)" -o "$(AGENTRED_LOCAL_BINARY)" ./cmd/agentred

# 构建 agentred Linux 版本(默认 linux/amd64，可覆盖 AGENTRED_GOOS/AGENTRED_GOARCH)
agentred-linux:
	mkdir -p "$(AGENTRED_BUILD_DIR)"
	GOOS=$(AGENTRED_GOOS) GOARCH=$(AGENTRED_GOARCH) CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o "$(AGENTRED_LINUX_BINARY)" ./cmd/agentred

# 通过 opsctl 部署 agentred 到远端(默认 local-coding:/usr/local/bin/agentred)
agentred-deploy: agentred-linux
	opsctl cp "$(AGENTRED_LINUX_BINARY)" "$(AGENTRED_TARGET):$(AGENTRED_REMOTE_TMP)"
	opsctl exec "$(AGENTRED_TARGET)" -- "install -Dm755 $(AGENTRED_REMOTE_TMP) $(AGENTRED_REMOTE_PATH) && rm -f $(AGENTRED_REMOTE_TMP) && $(AGENTRED_REMOTE_PATH) --help >/dev/null"
	@echo "已部署 agentred 到 $(AGENTRED_TARGET):$(AGENTRED_REMOTE_PATH)"

# 部署 agentred 到远端后重启裸进程(默认后台执行 agentred run，可覆盖 AGENTRED_RESTART_CMD)
agentred-deploy-restart:
	$(if $(strip $(AGENTRED_RESTART_CMD)),,$(error AGENTRED_RESTART_CMD must not be empty))
	$(MAKE) agentred-deploy
	opsctl exec "$(AGENTRED_TARGET)" -- "$(AGENTRED_RESTART_CMD)"
	@echo "已重启 $(AGENTRED_TARGET) 上的 agentred ($(AGENTRED_RESTART_CMD))"

agentred-deploy-local-coding:
	$(MAKE) agentred-deploy AGENTRED_TARGET=local-coding AGENTRED_GOOS=linux AGENTRED_GOARCH=amd64

agentred-local-coding: agentred-deploy-local-coding

# 生成 Wails 前端绑定
generate:
	"$(WAILS)" generate module

# 直接启动应用(生产构建,不监听文件变动)
run: build
ifeq ($(UNAME_S),Darwin)
	open build/bin/$(APP_NAME).app
else ifeq ($(OS),Windows_NT)
	./build/bin/$(APP_NAME).exe
else
	./build/bin/$(APP_NAME)
endif

# 安装前端依赖
install-deps:
	cd $(FRONTEND_DIR) && pnpm install

# 构建并安装应用到系统
install: build
ifeq ($(UNAME_S),Darwin)
	@if [ -w "$(MACOS_APP_INSTALL_DIR)" ]; then \
		mkdir -p "$(MACOS_APP_INSTALL_DIR)"; \
		ditto "build/bin/$(APP_NAME).app" "$(MACOS_APP_INSTALL_DIR)/$(APP_NAME).app"; \
	else \
		sudo mkdir -p "$(MACOS_APP_INSTALL_DIR)"; \
		sudo ditto "build/bin/$(APP_NAME).app" "$(MACOS_APP_INSTALL_DIR)/$(APP_NAME).app"; \
	fi
	@echo "已安装到 $(MACOS_APP_INSTALL_DIR)/$(APP_NAME).app"
else ifeq ($(OS),Windows_NT)
	@echo "Windows 安装暂未自动化；请运行 make build 后复制 build/bin/$(APP_NAME).exe。"
	@exit 1
else
	install -Dm755 "build/bin/$(APP_NAME)" "$(DESTDIR)$(PREFIX)/bin/$(APP_NAME)"
	@echo "已安装到 $(DESTDIR)$(PREFIX)/bin/$(APP_NAME)"
endif

# 运行前后端测试
test: test-backend test-frontend

# 运行后端测试
test-backend:
	go test $(BACKEND_PKGS)

# 运行前端测试
test-frontend: generate
	cd $(FRONTEND_DIR) && pnpm test

# E2E:Playwright 驱动真实 wails dev(-tags e2e 的确定性 fake runtime)跑 GUI 端到端。
# 详见 docs/e2e-harness-guide.md。一次性装依赖+浏览器:cd e2e && pnpm run setup(CI 在独立
# 步骤装,故这里不重复)。编排与收尾清理(回收残留 vite、删临时目录)都在 e2e/run-e2e.mjs
# 里用 Node 跨平台完成;配方只做 shell 无关的 cd && pnpm,cmd/sh 皆可。
e2e:
	cd e2e && pnpm test

# 临时功能验证:跑 e2e/scratch/ 里的一次性 spec(不提交)。约定/用法见 docs/e2e-harness-guide.md。
e2e-scratch:
	cd e2e && pnpm run test:scratch

# 测试覆盖率
test-cover:
	go test -coverprofile=coverage.out $(BACKEND_PKGS)
	go tool cover -html=coverage.out -o coverage.html
	@echo "覆盖率报告已生成: coverage.html"

# 前后端代码检查
lint: lint-backend lint-frontend

# 后端代码检查
lint-backend:
	golangci-lint run --timeout 10m

# 前端代码检查
lint-frontend: generate
	cd $(FRONTEND_DIR) && pnpm lint

# 前后端代码检查并自动修复
lint-fix: lint-fix-backend lint-fix-frontend

# 后端代码检查并自动修复
lint-fix-backend:
	golangci-lint run --timeout 10m --fix

# 前端代码检查并自动修复
lint-fix-frontend: generate
	cd $(FRONTEND_DIR) && pnpm lint:fix

# 本地完整检查
check: lint test

# 生成 mock(go.uber.org/mock)
mock:
	go generate ./...

# 清理构建产物
clean:
	rm -rf build/bin $(FRONTEND_DIR)/dist coverage.out coverage.html
