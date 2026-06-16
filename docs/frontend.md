# Frontend Conventions

React 19 + TS + Vite + Tailwind v4. Wails bindings are generated from `internal/app` into `frontend/wailsjs/` (gitignored).

## UI Components

**Frontend form controls go uniformly through shadcn `@/components/ui/*`.**

- Use `Select / SelectTrigger / SelectContent / SelectItem / SelectValue` for dropdowns (see `agent-backends.tsx` / `llm-providers.tsx`). Native `<select>` is **forbidden**.
- Input / Switch / Dialog / Button, etc. all use the wrappers in the ui directory.
- A native `<input type="radio">` may be kept when shadcn does not provide a styled equivalent, but before adding one, check whether the ui directory already has an equivalent component.

**Rationale:** theme color / dark mode / accessibility / keyboard interaction are all handled uniformly in the ui layer; native tags bypass the design tokens and end up producing two visual styles on the same page.

Before adding a component, check whether `frontend/src/components/ui` and `frontend/src/components/agentre` already have a primitive.

## i18n

New user-visible UI copy must be explicitly wired to i18n; do not add hardcoded Chinese.

- New UI copy uses `react-i18next`'s `useTranslation()` / `t("...")`, with keys placed in `frontend/src/i18n/locales/{zh-CN,en}/common.json`; both languages must be filled in at the same time.
- Do not introduce any bypass text-rewriting mechanism; static UI copy must be wired explicitly to `t(...)` in the component or module.
- Do not translate dynamic content such as agent output, user input, terminal output, file contents, diffs, code blocks, or markdown rendering; by nature it never enters `t(...)`.
- `eslint-plugin-i18next`'s `i18next/no-literal-string` catches hardcoded Chinese copy in JSX text and in visible attributes such as `aria-label` / `title` / `placeholder` / `alt`; if you need to display copy, change it to `t(...)`.
- After changing i18n resources, run:

```bash
cd frontend && pnpm test -- src/__tests__/i18n.test.ts src/__tests__/eslint-i18n.test.ts
cd frontend && pnpm exec eslint src
```

## Project Structure

- `frontend/components.json` defines the aliases: `@/components`, `@/lib/utils`, `@/components/ui`, `@/lib`, `@/hooks`.
- Routing uses `MemoryRouter`.
- Stores live in `frontend/src/stores`, hooks in `frontend/src/hooks`.
- Wails runtime / bindings are imported from `frontend/wailsjs`.
- Keep the existing dense desktop-app layout for the UI; **do not** write landing-page styling into the app shell.
- For icons used in user operations, prefer the `lucide-react` and Iconify Tabler already in use in the project; **do not** hand-draw inline SVG.

## Package Management

`pnpm` is the source of truth; **do not** use npm.

```bash
cd frontend && pnpm install              # install dependencies
cd frontend && pnpm add <pkg>            # add a package
cd frontend && pnpm remove <pkg>         # remove a package
cd frontend && pnpm test                 # vitest (happy-dom)
cd frontend && pnpm test -- path/to/file.test.tsx   # single file
```

`make test-frontend` runs `make generate` first and then `pnpm test`; use it when the wails bindings need to be regenerated. Vitest is configured with happy-dom and aliases the wails imports to a mock, so most tests can run even without a `frontend/wailsjs/` directory.

## Formatting / Lint

Go:

```bash
gofmt -w <files>
goimports -w <files>       # local-prefixes: agentre
make lint                  # golangci-lint + frontend ESLint
make lint-fix              # auto-fix (use on a small scope)
```

`goimports` groups local imports under the `agentre` prefix, matching `.golangci.yml`.

Frontend: follow the existing TS/CSS style; **do not** introduce a large formatting-only diff.

## Module Path

The Go module is `agentre`; **do not** invent a `github.com/...` prefix.
