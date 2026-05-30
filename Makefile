.PHONY: run dev build build-windows agentred agentred-linux agentred-deploy agentred-deploy-restart agentred-deploy-local-coding agentred-local-coding generate test test-backend test-frontend test-cover lint lint-backend lint-frontend lint-fix lint-fix-backend lint-fix-frontend mock install install-deps clean check

APP_NAME := Agentre
VERSION ?= 0.1.0
COMMIT_ID := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
VERSION_PKG := github.com/cago-frame/cago/configs
BUILDINFO_PKG := agentre/internal/buildinfo
LDFLAGS := -s -w -X $(VERSION_PKG).Version=$(VERSION) -X $(BUILDINFO_PKG).CommitID=$(COMMIT_ID)
UNAME_S := $(shell uname -s 2>/dev/null || echo unknown)
FRONTEND_DIR := frontend
BACKEND_PKGS := $(shell go list ./... | grep -v '/frontend/')
MACOS_APP_INSTALL_DIR ?= /Applications
PREFIX ?= /usr/local
WAILS_PLATFORM ?=
WAILS_BUILD_FLAGS ?=
WAILS ?= $(shell command -v wails 2>/dev/null || printf "%s/bin/wails" "$$(go env GOPATH)")
WINDOWS_PLATFORM ?= windows/amd64
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

# 构建生产版本(默认当前平台；可用 WAILS_PLATFORM=windows/amd64 跨平台构建)
build:
	"$(WAILS)" build -ldflags="$(LDFLAGS)" $(if $(strip $(WAILS_PLATFORM)),-platform "$(WAILS_PLATFORM)") $(WAILS_BUILD_FLAGS)

# 构建 Windows 版本(默认 windows/amd64，可覆盖 WINDOWS_PLATFORM=windows/arm64)
build-windows:
	$(MAKE) build WAILS_PLATFORM="$(WINDOWS_PLATFORM)" WAILS_BUILD_FLAGS="$(WAILS_BUILD_FLAGS)"

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
	go test ./...

# 运行前端测试
test-frontend: generate
	cd $(FRONTEND_DIR) && pnpm test

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
