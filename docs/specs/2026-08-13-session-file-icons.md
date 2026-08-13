# 会话文件类型图标

> Status: Approved — Logo A visual revision
> Owner: chat experience（前端）
> Last updated: 2026-08-13

**Objective:** 让用户在会话右侧文件相关界面中通过一致的彩色类型图标快速识别常见编程语言、文档、数据、媒体和归档文件，同时不改变任何文件的预览与操作能力。

**Hard invariant:** 文件类型图标只表达文件身份，不表达 Git 状态、可预览性或操作成功与否；不支持预览的文件即使有精确图标，也必须继续保持现有不可预览行为。

## Problem

1. **不同编程语言显示为同一个代码图标。** `frontend/src/components/agentre/chat-context-sidebar/views/sidebar-row.tsx` 的 `FileTypeIcon` 只按 `markdown / image / code / other` 四档渲染 Lucide 图标，因此 `.go`、`.py`、`.ts` 与 `.js` 无法从图标区分；用户明确要求按后缀显示合适图标。
2. **常见非代码格式没有身份信息。** PDF、Office、CSV、压缩包、音视频、字体与数据库等当前全部落入通用文件档；现有测试也明确把 `archive.zip` 断言为 `other`，精确信息没有被展示。
3. **同一文件在不同表面使用不同的粗粒度图标入口。** 文件树和搜索结果使用 `FileTypeIcon`，预览标签、溢出菜单与预览标题使用 `PREVIEW_KIND_ICON`，Git 扁平列表只显示状态字母；若分别扩展，会产生同一路径在不同位置显示不一致的风险。
4. **图标分类与可预览性当前耦合。** 现有 `FileTypeIcon` 直接复用 `previewKind`，但 PDF、ZIP 和 DOCX 等需要准确图标，却仍不应进入现有预览器；继续把两者绑定会迫使视觉识别与文件能力互相牵制。

## Actors and user stories

1. 作为浏览 agent 改动的用户，我希望一眼区分 Go、Python、TypeScript、JavaScript 等文件，以便更快扫描文件树。
2. 作为查看普通项目文件的用户，我希望 PDF、Office、CSV、压缩包、媒体和配置文件也有准确图标，以便不必先读完整后缀。
3. 作为在变动、目录、Git、搜索与预览之间切换的用户，我希望同一文件始终显示相同的类型身份，以免重新判断。
4. 作为键盘或读屏用户，我希望图标只是装饰信息，文件名、Git 状态和可操作性仍由既有文字与语义表达。

## Design decisions

| # | Decision | Basis and rejected option |
|---|---|---|
| 1 | 复用现有 `@iconify/react` 渲染层与已安装的 `@iconify-icons/tabler` 静态图标数据，不增加新的图标框架或网络加载 | 项目已离线打包 Iconify，Tabler 包已包含主要语言、PDF、Office、CSV、ZIP、XML、SQL、SVG、图片、媒体等图标。拒绝：引入完整 VS Code 图标主题——增加第二套图标数据风格与约 3.8MB 安装包内容，且内嵌品牌色不受项目 token 控制 |
| 2 | 建立一个统一的文件类型分类器和一个统一的文件图标组件，所有文件相关表面消费同一结果 | 现状已有两个粗粒度入口，继续局部扩展会漂移。拒绝：在文件树、Git、预览标签各写一张后缀表 |
| 3 | 识别优先级为完整文件名、复合文件名/后缀、普通后缀、语言识别回退、通用文件回退，匹配不区分大小写 | `Dockerfile`、`pnpm-lock.yaml`、`.env.local`、`.d.ts`、`.tar.gz` 不能由最后一个后缀准确表达。拒绝：只取最后一个点后的后缀 |
| 4 | 文件类型分类与 `previewKind` / `resolvePreviewRelPath` 严格分离 | 用户确认 PDF、ZIP、Office 等应有精确图标，但现有预览能力不扩展。拒绝：为了显示图标把这些格式加入预览 allowlist |
| 5 | 采用 Logo A 的无容器彩色图标：17px 仅作为对齐槽位，内部直接渲染 16–17px Tabler Brand Logo 或文件类型 glyph，不显示背景、描边、圆角或文字缩写；颜色来自少量语义化文件身份 token，未知类型回退中性灰 | 用户在看到小方块实装截图后否定其背景与边框感，并批准真实 Logo A。直接着色的品牌/类型字形在密集侧栏中保留语言辨识度，同时减少视觉噪声。拒绝：彩色背景小方块；拒绝：用 `Go / TS / Py` 文本缩写冒充最终图标；拒绝：只使用同形状的通用文件线图标；拒绝：硬编码品牌十六进制 |
| 6 | Git 状态字母与文件类型图标同时保留，二者不互相着色或替代 | `M/A/D/R` 表达状态，文件图标表达类型；两者含义独立。拒绝：用文件图标颜色承载 Git 状态 |
| 7 | 首轮提供精选的高频格式集合，而非声明支持完整图标主题；未知格式稳定回退 | 常见仓库的扫描收益集中在语言、配置、文档、数据、媒体、归档与项目入口文件。拒绝：维护数百种冷门格式与专用图标 |

## 文件类型识别

当文件路径可见时，分类器先取不区分大小写的 basename，再按以下顺序决定身份；命中后不继续向下匹配。

**完整或特定文件名**包括：`Dockerfile`、`Makefile`、`CMakeLists.txt`、`package.json`、`package-lock.json`、`pnpm-lock.yaml`、`yarn.lock`、`go.mod`、`go.sum`、`Cargo.toml`、`Cargo.lock`、`requirements.txt`、`pyproject.toml`、`Gemfile`、`.gitignore`、`.gitattributes`、`.editorconfig`、`.npmrc`、`.prettierrc` 与 `.eslintrc`。这些入口显示所属生态或配置身份，而不是退化成 JSON、YAML、TOML 或普通文本。

**复合形式**至少包括 `.d.ts`、`.env.*`、`.tar.gz` 与 `*.dockerfile`；它们分别显示 TypeScript、环境配置、归档和 Docker 身份。

**编程语言和 Web 格式**覆盖现有 Monaco 语言表中的高频语言：Go、Python、TypeScript、JavaScript、React TS/JS、Rust、Java、Kotlin、C/C++、C#、Shell、HTML、CSS/Sass、PHP、Swift、SQL、Markdown、JSON、YAML、TOML、XML 及常见纯文本/配置格式。`.tsx` 与 `.jsx` 使用 React 字形，并分别使用 TypeScript 与 JavaScript 的色调；同一语言的扩展名别名共享身份。

**文档和表格**覆盖 PDF、Word（DOC/DOCX）、Excel（XLS/XLSX）、PowerPoint（PPT/PPTX）、CSV 与纯文本/日志。

**媒体和字体**覆盖现有可预览图片格式，并单独识别 SVG；同时识别常见音频、视频和字体后缀。准确图标不承诺新增预览能力。

**归档和二进制**覆盖 ZIP、7Z、RAR、TAR、GZ/TGZ、BZ2、XZ，以及常见可执行文件、动态库、WebAssembly、目标文件和 Java 归档。不能更精确判断时使用中性通用文件身份。

**数据库、密钥和锁文件**覆盖 SQLite/DB、证书与密钥后缀、`*.lock`；图标仅表达格式，不改变敏感文件的显示、打开或日志规则。

路径为空、只有目录分隔符、未知扩展名、无法识别的 dotfile 或大小写混合输入都不得抛错；它们使用通用文件回退。Windows 与 POSIX 路径分隔符都必须正确提取 basename。

## 视觉与主题

文件图标使用固定 17px 的透明对齐槽位，位于现有文件名左侧图标列，不改变树缩进、行高、菜单槽位或文字截断优先级。槽位本身没有背景、描边、圆角、阴影或内边距；内部 Iconify glyph 直接缩放到 16–17px，并通过 `currentColor` 着色。选中、hover 和聚焦背景只属于整行或既有标签，不为图标增加单独底板。

高辨识度语言和生态优先使用已安装 Tabler 集合中的品牌/语言 Logo，例如 Go、Python、TypeScript、JavaScript、React、Rust 与 npm；文档、配置、数据、媒体、归档和未知格式使用对应的文件类型 glyph。最终图标不得以 `Go / TS / Py / JS` 等文本缩写代替 Logo；没有专用品牌 glyph 的语言使用其已登记的通用代码图标与身份色安全回退。

颜色使用少量可复用的文件身份 token，而不是每个扩展名单独拥有一个颜色：蓝、黄、青、紫、橙、绿、红和中性。每个 token 同时定义浅色与深色值，并由设计系统文档记录；组件通过 `text-file-<tone>` 将 token 应用于 glyph，不使用 `bg-file-<tone>`。颜色只帮助扫描，Logo 或文件类型 glyph 始终提供第二重非颜色线索。

目录不是文件类型。折叠目录继续使用中性的 `Folder` 与 `ChevronRight`，展开目录使用中性的 `FolderOpen` 与 `ChevronDown`；目录图标不消费文件分类器，不继承语言身份色，也不因本轮视觉修改改变尺寸、缩进或展开交互。目录搜索结果若代表目录，同样继续显示中性文件夹图标。

图标自身为装饰元素并隐藏于辅助技术。文件行、标签和标题继续由文件名提供可访问名称；Git 状态继续由现有隐藏文字标签提供语义。图标不会增加 tooltip、可聚焦点或点击目标。

未知文件使用中性通用文件图标；加载图标不依赖 Iconify 网络 API，所有使用的图标数据静态导入并随桌面应用打包。

## 应用范围与行为

统一组件应用于会话右侧的变动树、完整目录树、目录搜索结果、Git 文件列表，以及文件预览标签、标签溢出菜单和预览标题。无论文件从哪个入口出现，类型身份保持一致。

在 Git 列表中，状态字母仍位于现有状态列，文件类型图标新增在文件名之前；窄侧栏下目录后缀仍先收缩，文件名的现有优先级不降低。目录行仍使用现有文件夹图标，不参与文件类型分类。

当精确图标对应的格式不在预览 allowlist 内时，该文件行保持现有不可预览形态：不响应预览单击、不显示预览菜单项，但复制路径、系统打开与文件管理器显示等既有能力不变。不会新增 PDF、Office、归档、音视频、字体、数据库或二进制内容预览器。

本轮不新增可见文案，因此无需新增 i18n key；若实现过程中出现任何新提示或标签，必须返回规格修订并同时提供中英文资源。

## 失败、兼容与性能

分类是纯本地、同步且确定性的路径解析，不读取文件内容、不访问文件系统、不调用后端，也不把完整路径发送给第三方。不存在单独的加载或错误状态。

静态图标按单图标模块导入，构建产物只包含实际使用的图标数据；不得导入整套 Iconify collection 或在运行时按字符串向网络取图。旧的本地持久化状态和后端协议不受影响。

新增分类应保持开放扩展：增加格式只需在统一分类器中登记，而无需修改每个消费表面。但不为未来图标包、用户自定义主题或插件机制预留当前没有需求的扩展接口。

## Out of scope

- 为 PDF、Office、归档、音视频、字体、数据库或二进制文件新增内容预览器。
- 根据文件内容、magic bytes、MIME 或 shebang 猜测无后缀文件；本轮只使用路径信息。
- 完整复刻 VS Code、Material Icon Theme 或任一第三方文件主题的全部格式集合。
- 用户自定义文件图标、颜色主题或覆盖规则。
- 修改目录图标、Git 状态语义、文件行点击、菜单或预览标签行为。
- 在聊天正文、终端输出、Markdown 文本中的路径引用上增加同类图标。

## Testing decisions

| Seam | What it verifies | Prior art |
|---|---|---|
| 文件类型分类纯函数 | 完整文件名优先级、复合后缀、普通扩展名、大小写、Windows/POSIX 路径、语言与常见格式映射、未知输入回退 | `frontend/src/lib/file-preview/monaco-language.test.ts` 的路径语言表驱动测试 |
| 统一文件图标组件 | 分类结果选择正确静态 Logo/glyph 和 `text-file-*` 色调、17px 透明槽位、16–17px glyph、无背景/描边/圆角/文字缩写、装饰性无障碍属性、未知类型中性回退 | `frontend/src/components/agentre/file-type-icon.test.tsx` 现有分类与组件测试 |
| 文件树/搜索/Git 行渲染 | 三种列表都消费统一组件；Git 状态字母与类型图标共存；不可预览的 PDF/ZIP 仍不可点击预览 | `row.test.tsx`、`files-panel.test.tsx`、`git-tab.test.tsx` 现有行交互与不可预览断言 |
| 预览表面渲染 | 标签、溢出菜单与标题对同一路径显示一致类型身份，不改变标签交互 | `frontend/src/components/agentre/file-preview/preview-tab-strip.tsx` 与现有 file-preview 测试 |
| 主题与依赖守卫 | 新色彩仅来自成对浅/深 token；静态单图标导入，不使用在线 Iconify 字符串加载或整 collection | `frontend/src/styles/globals.css` / `docs/design.md` 的 token 约定与源码复核 |

自动化不适合判断 16–17px Logo/glyph 在实际 WebView 的主观清晰度。实现完成后需要在真实浅色与深色主题、常规与窄侧栏下观察 Go、Python、TypeScript、JavaScript、React、Rust 等代码 Logo，以及 PDF、归档、媒体、未知文件和带 Git 状态的文件行；同时确认图标没有独立背景/边框，目录仍显示中性的真实文件夹与 Chevron，并复核 Logo A 的密度和层级是否保持。

## Links

- 已批准视觉 mockup（本地产物，不在 Git）：`.dev-kit/artifacts/2026-08-13-session-file-icons/mockups/logo-icon-a.png`
- 可运行 mockup 源（本地产物，不在 Git）：`.dev-kit/artifacts/2026-08-13-session-file-icons/mockups/logo-icon-a.html`
- 前置文件面板设计：[`2026-08-09-session-files-ux-refine.md`](./2026-08-09-session-files-ux-refine.md)
- 设计系统：[`../design.md`](../design.md)
- 前端约定：[`../frontend.md`](../frontend.md)

## Open questions

无。
