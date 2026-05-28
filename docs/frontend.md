# Frontend Conventions

React 19 + TS + Vite + Tailwind v4。Wails 绑定从 `internal/app` 生成到 `frontend/wailsjs/`（gitignored）。

## UI 组件

**前端表单控件统一走 shadcn `@/components/ui/*`。**

- 下拉框用 `Select / SelectTrigger / SelectContent / SelectItem / SelectValue`（参考 `agent-backends.tsx` / `llm-providers.tsx`）。**禁止**新增原生 `<select>`。
- Input / Switch / Dialog / Button 等都用 ui 目录里的封装。
- 原生 `<input type="radio">` 在 shadcn 没提供样式时可以保留，但新增前先看 ui 目录里有没有等价组件。

**理由：** 主题色 / 暗色 / 无障碍 / 键盘交互都在 ui 层统一处理，原生标签会绕过设计 token，导致同一页面里出现两套视觉风格。

新增组件前先看 `frontend/src/components/ui` 和 `frontend/src/components/agentre` 是否已经有原语。

## 项目结构

- `frontend/components.json` 定义 alias：`@/components`、`@/components/ui`、`@/lib`、`@/hooks`。
- 路由用 `MemoryRouter`。
- Stores 放 `frontend/src/stores`，hooks 放 `frontend/src/hooks`。
- Wails runtime / bindings 从 `frontend/wailsjs` 导入。
- UI 保持现有 dense desktop-app layout，**不要**在 app shell 里写 landing-page 风格。
- 用户操作的 icon 优先用项目里已经在用的 `lucide-react` 和 Iconify Tabler，**不要**手画 inline SVG。

## 包管理

`pnpm` 是 source of truth，**不要**用 npm。

```bash
cd frontend && pnpm install              # 装依赖
cd frontend && pnpm add <pkg>            # 加包
cd frontend && pnpm remove <pkg>         # 删包
cd frontend && pnpm test                 # vitest（happy-dom）
cd frontend && pnpm test -- path/to/file.test.tsx   # 单文件
```

`make test-frontend` 会先跑 `make generate` 再 `pnpm test`，需要重新生成 wails binding 时用它。Vitest 配置了 happy-dom 并把 wails 导入 alias 到 mock，所以即使没 `frontend/wailsjs/` 目录也能跑大多数测试。

## 格式化 / Lint

Go：

```bash
gofmt -w <files>
goimports -w <files>       # local-prefixes: agentre
make lint                  # golangci-lint + frontend ESLint
make lint-fix              # 自动修复（小范围用）
```

`goimports` 把本地 import 分组在 `agentre` 前缀下，匹配 `.golangci.yml`。

前端：跟现有 TS/CSS 风格走，**不要**引入大块 formatting-only diff。

## 模块路径

Go module 是 `agentre`，**不要**编造 `github.com/...` 前缀。
