# agentred 安装引导与远端 UI/UX 优化

<!-- File: docs/specs/2026-08-11-agentred-onboarding.md -->

> Status: Approved（决策 1 的「只在页空态内出现」、用户故事 4 的实现方式与设计段 A 的同一前置已被
> [2026-08-14 加下一台设备的引导：随时可达，不只在空态](./2026-08-14-add-device-guidance.md) 取代，
> 见下方逐条标注；其余部分仍然有效）
> Owner: 桌面端前端（远端设备页）+ cmd/agentred + 发布流水线
> Last updated: 2026-08-11

**Objective:** 让用户能从零开始，在「远端设备」设置页按「安装 → 启动服务 → 配对」三步引导，把一台远端机器上的 agentred 安装、注册为后台服务并配对到桌面；发布流程产出可下载的 agentred 独立资产与安装脚本；agentred 提供后台服务生命周期管理命令。

**Hard invariant:** 现有已配对设备的配对安全（6 位一次性配对码、TOFU 指纹、keychain token）、LAN/Relay 连接池、watcher 在线状态（`remote.device.state`）与设备列表行为一律不变；桌面端不新增对远端机器的 shell 访问通道，也不实时拉取远端服务状态；Unix 本地 IPC 路径与权限不变，Windows named pipe 只能扩展同等的当前用户本机边界；用户从前台 `run` 切换到后台服务时，自定义数据目录、host/port/TLS/Relay server 配置不得丢失；未执行任何引导操作的老用户，「远端设备」页原有能力不回归。

> 交互方向与信息层级已通过本地 mockup 确认（非功能占位，见 `.dev-kit/artifacts/2026-08-11-agentred-onboarding/mockups/`，不提交到 Git）。绑定决策以本文为准。

## Problem

1. **首次使用远端的空态要求执行 `agentred run` / `agentred pair`，但 agentred 从哪来、怎么安装、怎么验证、怎么保持在线没有任何入口。** 空态只有两段 `<code>` 提示（`frontend/src/components/agentre/remote-devices/remote-devices-panel.tsx:307-308`），假设 agentred 已装好、能一直跑着。
2. **agentred 没有后台服务能力。** `agentred run` 是前台进程（`cmd/agentred/run.go`），SIGINT/SIGTERM 即停止；`root.go` 的子命令只有 `run / status / pair / login / unclaim / llm / claudecode`，没有 `service` 生命周期命令，也没有 `--version`。用户只能开着终端挂一个进程。
3. **发布流程不产出 agentred 独立资产。** `.github/workflows/release.yml` 与 `nightly.yml` 只构建桌面（DMG / DEB / NSIS installer），README 却写“Agentre includes agentred”；用户无法从正常发布通道获得 agentred 二进制，只能源码 `go build`。
4. **无法验证安装或排查版本。** 没有 `agentred --version`；daemon 的 `/local/status`（`cmd/agentred/status.go`）不返回版本字段，UI 也没有版本信息可展示。
5. **远端页初始加载失败被误显示成「还没有设备」。** `if (loading) return null`（`remote-devices-panel.tsx:43 / 89`）在加载时整体空白；首次 `reload()` 失败既无错误态，Promise rejection 还可能成为未处理 rejection，最终被当作用户没配设备。
6. **引导里的命令不可复制。** `agentred pair` 等只是 `<code>`（`add-device-dialog.tsx:114`），而应用 body 默认 `user-select:none`，用户难以选中复制。
7. **`cmd/agentred/README.md` 已过时。** 仍声称 stateless / no SQLite / single binary，与当前实现（daemon 持有独立 `agentred.db`、持久化 session journal）不符，误导安装与运维方式。
8. **Windows agentred 目前只能交叉编译、不能运行。** `internal/daemon/ipc.go:25-29` 在 Windows 直接返回 `ipc: windows named pipe path not yet wired`，CLI 客户端也只会拨 Unix socket（`cmd/agentred/client.go:15-22`）；不补 named-pipe 本地 IPC，Windows 的 `run / status / pair / llm / service` 和发布资产都无法兑现。
9. **后台服务没有可靠的运行配置来源。** 当前 `run` 只用本次 flag/环境构造 daemon options（`cmd/agentred/run.go:48-67`），未读写 `state.Listen`；`login --server` 也不持久化 Relay server。服务若只执行裸 `agentred run`，会丢失自定义 host/port/TLS/Relay 和非默认数据目录。

## Actors and user stories

1. 作为**首次使用远端的用户**，我想在「远端设备」页按「安装 → 启动服务 → 配对」逐步被引导，以便从零接入一台远端机器，而不是面对两句命令猜流程。
2. 作为**需要长期运行远端的用户**，我想把 agentred 注册为开机自启的后台服务，以便终端关闭也不离线。
3. 作为**想手动安装的用户**，我想按平台看到可复制、可验证的安装命令。
4. 作为**已配好设备的老用户**，我不希望引导打扰我；设备列表、在线状态、添加/管理能力照常工作。（本条的**实现方式**被 [2026-08-14 加下一台设备的引导](./2026-08-14-add-device-guidance.md) 决策 1 取代：原先靠「有设备时引导根本不出现」满足，现在靠「引导不常驻、只在用户从唯一入口召唤时才展开」满足——不打扰仍然成立，但引导对老用户随时可达。）

## Design decisions

| # | Decision | Basis and rejected option |
|---|---|---|
| 1 | **三步引导放在「远端设备」页~~空态~~内（页面内步骤条展开）**，不新建独立页面或弹窗。（「只在空态内出现」这一限定被 [2026-08-14 加下一台设备的引导](./2026-08-14-add-device-guidance.md) 决策 1 取代：设备列表为空时引导自动展开且不提供收起；已有设备时默认收起，由页头唯一的「添加 agentred」入口召唤后展开在仍然可见的设备列表上方，并可收起。本行其余部分——页面内步骤条、不新建独立页面或弹窗——不变。） | 信息量大、需要跨步骤状态与命令复制，页面内步骤条最稳，且与设置页既有布局一致。Rejected: 弹窗 — 塞不下命令+清单；独立路由 — 打断设置页上下文。 |
| 2 | **新增 agentred 后台服务闭环**：`service install/start/status/restart/stop/uninstall`；Linux 用 user 级 systemd（尝试 `loginctl enable-linger`）、macOS 用 launchd LaunchAgent、Windows 用用户级计划任务，默认不创建系统级服务。 | 真正的闭环，优先用户级以减少权限要求；Linux host policy 不允许 linger 时，注册仍成功但必须输出可执行的修复命令。Rejected: 只做前台引导 — 运维门槛仍在；系统级 systemd / Windows Service — 默认需要管理员，跨平台体验割裂。 |
| 3 | **发布流程新增 agentred 独立资产构建**：release 与 nightly 各加一个 job，产出 `agentred-<version>-<goos>-<goarch>`（darwin/linux 用 tar.gz、windows 用 zip）+ `SHA256SUMS`，并保留 `SHA256SUMS.txt` 兼容现有桌面更新，上传到同一个 GitHub Release。 | 桌面与远端不是同一台机器，打进桌面安装包无意义；独立资产是「可从正常通道获得」的前提。Rejected: 只改 README — 仍没有可安装产物。 |
| 4 | **新增 `install.sh`（POSIX sh）与 `install.ps1`（Windows）作为发布资产**，UI 显示真实一键安装命令；脚本负责识别 OS/arch、下载对应资产、校验 SHA256SUMS；Unix 安装到可写的 `/usr/local/bin`，不可写则 `~/.local/bin` 并提示 PATH；Windows 安装到 `%LOCALAPPDATA%\Programs\agentred\` 并写入用户 PATH。 | 一键入口命令简短、可用，Windows 不要求管理员 PowerShell。Rejected: 只显示手动下载命令 — 又长又易错；不校验 — 无法保证完整性。 |
| 5 | **新增 `agentred --version`（semver + commit）**；daemon `/local/status` 增加 `version` 字段（向后兼容，老实现无该字段时正常），账号登录登记同一真实版本而非固定 `dev`。安装步骤的验证清单用 `agentred --version`。 | 安装可验证、可排查，账号设备版本也不能与二进制身份分叉。Rejected: 不做版本 — 装没装上、哪个版本无从判断。 |
| 6 | **桌面端不实时拉取远端服务状态**：第二步只给命令 + 「在远端终端自行验证」清单；配对成功后在线状态由现有 watcher（`remote.device.state`）展示。 | 配对前桌面不知道远端地址、也无 shell 通道，实时探测不成立；配对后在线状态已存在。Rejected: 给 watcher 新增服务状态 RPC — 超本轮范围。 |
| 7 | **远端页加载失败显示错误态 + 重试**，不再被当作空态。 | 消除「加载失败伪装成没有设备」。Rejected: 维持 `return null` — 掩盖故障。 |
| 8 | **Windows 本地 IPC 扩展为当前用户 ACL 的 named pipe**，Unix socket 的路径、HTTP 路由和 JSON 形状保持不变；daemon 与 CLI 共享同一 endpoint 解析。 | 完整跨平台服务的必要前置。Rejected: 仍发布 Windows 资产但不接 IPC — 二进制启动即退出，属于不可用产物。 |
| 9 | **agentred 运行配置收敛到 state**：显式 flag → 环境 → 持久化 state → 默认值；显式变更写回 state，`login --server` 持久化 Hub server URL；服务定义只运行当前二进制的 `run` 并固定解析后的 `AGENTRED_DATA_DIR`。 | 避免 systemd/launchd/计划任务各复制一套参数规则，也避免从前台切后台时丢配置。Rejected: 把当前 flags 全写进三种服务 manifest — 重复、易漂移。 |

## Design

### A. 首次使用：三步引导（「远端设备」页空态）

前置：~~设备列表为空（未配对任何设备）时，~~「远端设备」页显示三步步条（安装 agentred → 启动远端服务 → 配对并验证），当前步骤展开；步骤完成标记 ✓。~~有设备时完全走现有设备列表，不出现引导。~~ **引导的出现条件（含本节标题里的「页空态」）已被 [2026-08-14 加下一台设备的引导](./2026-08-14-add-device-guidance.md) 决策 1 取代**：设备列表为空时引导自动展开且不提供收起；已有设备时默认收起，由页头唯一的「添加 agentred」入口召唤后展开在仍然可见的设备列表上方，并可收起。

- **步骤 1 · 安装**：平台选择（Linux / macOS / Windows）。选中的平台决定显示的安装命令（带复制按钮，`data-selectable-text`，复制成功 toast）：
  - Linux / macOS：`curl -fsSL https://github.com/agentre-ai/agentre/releases/latest/download/install.sh | sh`
  - Windows：`irm https://github.com/agentre-ai/agentre/releases/latest/download/install.ps1 | iex`（普通 PowerShell）
  - 提供「手动安装」退路入口。点击「我已安装，下一步 →」进入步骤 2。
- **步骤 2 · 启动服务**：运行方式切换「后台服务（推荐）/ 前台临时运行」。
  - 后台服务：`agentred service install --start`、`agentred service status`、`agentred service restart` 各带复制按钮；「确认服务已运行」清单（编号 1/2/3）：在远端终端运行 `agentred service status` 看到 `Daemon running`、确认监听地址含 `ws://…:7456/rpc`、保持服务运行。
  - 前台运行：`agentred run` 复制，提示保持终端开启、关闭即停止。
  - 点击「服务已运行，下一步 →」进入步骤 3。
- **步骤 3 · 配对**：`agentred pair` 复制；连接地址 + 6 位配对码输入（可选名称），字段与校验规则与现有 `AddDeviceDialog` 一致（`URL_RE` / 6 位大写码）；提交走现有配对流程（`RemoteDeviceAdd`），失败在步骤内展示具体原因；成功后设备出现在列表并显示在线（现有 watcher），引导行提示「下一步配置 Agent 后端」。

### B. agentred 后台服务闭环（`cmd/agentred service`）

子命令（cobra 挂到 `agentred service` 下，`root.go` 注册）：

| 命令 | 可观测行为 |
|---|---|
| `agentred service install [--start]` | 注册开机自启服务（Linux: user systemd unit + `loginctl enable-linger`；macOS: `~/Library/LaunchAgents/` LaunchAgent；Windows: 计划任务），可选 `--start` 立即启动。重复 install 幂等（更新配置）。 |
| `agentred service start` / `stop` / `restart` | 启动 / 停止 / 重启已注册服务；失败输出明确错误与恢复提示。 |
| `agentred service status` | 报告服务注册状态与运行状态（如 systemd `active` / launchd `loaded`+`running` / 计划任务状态），机器可读输出用于引导清单核对。 |
| `agentred service uninstall` | 移除服务注册（并停止）；幂等。 |

- 跨平台实现收敛到一个 `ServiceManager` 接口，按 GOOS 选择 systemd / launchd / 计划任务实现；`run`（前台）保持现有命令面。
- 命令输出为纯文本、稳定格式：`Daemon running` / `Daemon stopped` / `Service not installed` 为首行状态词，后接细节；失败退出码非 0 且带原因。
- 服务启动实际就是以后台方式拉起当前二进制的 `agentred run`，设置解析后的 `AGENTRED_DATA_DIR`；host/port/TLS/Hub server 由 state 统一恢复。
- `run` 的运行配置优先级为显式 flag → 环境 → 持久化 state → 默认值；显式变更写回 state；`login --server` 成功后持久化 Hub server URL。
- Windows local IPC 使用当前用户 ACL 的 named pipe；`run / pair / status / llm` 的 HTTP 路径与 JSON 形状与 Unix 完全一致，CLI 与 daemon 共享 endpoint 解析，Unix socket 行为不变。

### C. 版本与发布

- `agentred --version` 输出 `agentred <semver> (<commit>)`；版本经 ldflags 注入（`configs.Version` + `internal/buildinfo.CommitID`），与桌面一致。
- daemon `/local/status` 应答新增 `version` 字段（老 daemon 无该字段时 `agentred status` 照常工作、不报错）。
- `.github/workflows/release.yml` 与 `nightly.yml` 各新增 agentred 资产构建 job：复用现有 goos/goarch 矩阵（darwin/linux/windows × amd64/arm64），产出 `agentred-<version>-<platform>`（tar.gz / zip）+ `SHA256SUMS`，同时上传内容相同的 `SHA256SUMS.txt` 兼容现有 `update_svc`，并上传 `install.sh` / `install.ps1`，随现有 Release 发布（nightly 传 nightly release）。
- Windows agentred 资产只在 named-pipe IPC 与计划任务服务可用后发布，不能发布“能构建但启动即失败”的二进制。
- `install.sh` / `install.ps1` 行为：识别 OS/arch → 从当前发布通道下载对应 agentred 资产 → 校验 SHA256SUMS → Unix 安装到 `/usr/local/bin`（不可写则 `~/.local/bin` 并提示加入 PATH），Windows 安装到 `%LOCALAPPDATA%\Programs\agentred\` 并写入用户 PATH → 以 `agentred --version` 完成确认。
- workflow 的 ldflags 统一使用完整模块路径 `github.com/agentre-ai/agentre/internal/buildinfo.CommitID`，release/nightly 与 Makefile 不各自维护不同版本注入规则。

### D. 页面状态与交互修复

- 加载中：居中 spinner + `role="status"`，不再整页空白。
- 首次加载失败：错误卡（`border-status-error/40 bg-destructive-soft` + 重试按钮），明确不是「还没有设备」，不产生未处理 rejection。
- 命令与配对码等需复制的文本：带复制按钮 + `data-selectable-text`（body 默认 `user-select:none`）。
- 设备列表、在线状态、LAN/Relay 路径、TLS 徽标、Provider 同步、未认领提示、解除配对等现有行为不变。

### E. i18n

- 全部新增 UI 文案走 `t(...)`，同步更新 `frontend/src/i18n/locales/zh-CN/common.json` 与 `en/common.json`（`remoteDevices` namespace 及新增键）；不硬编码中文。动态输出（命令、设备名、状态）不进入 `t(...)`。
- 静态命令文本作为 UI 展示文案随 i18n 管理；复制到剪贴板的内容保持原样（命令本身不翻译）。

## Out of scope

- 桌面 SSH / 一键远程部署（把 agentred 直接推到远端机器）。
- 桌面端对远端服务状态的实时查询（新增 RPC / watcher 扩展）。
- agentred 自更新（`agentred upgrade`）。
- daemon outdated 的完整升级闭环（保留现有布尔警告与提示文案，不新增下载/升级入口）。
- 账号中 account-only agentred 的设备行、自动局域网发现。
- 解除配对时对 daemon 调用 revoke / 清理 `pairedPeers`。
- 桌面应用自身安装包中捆绑 agentred。

## Testing decisions

| Seam | 验证内容 | 参考 |
|---|---|---|
| Go：构建身份与 local status | `--version` 稳定格式；`/local/status.version`；老 daemon 无字段兼容；账号登录登记真实版本 | `cmd/agentred/main_test.go`、`status_test.go`、`login_test.go`、`internal/daemon/daemon_test.go` |
| Go：`agentred service` 命令层 | 子命令解析、`--start` 编排、稳定首行状态、错误与幂等；注入 fake `ServiceManager` 断言调用序列 | 新建 `cmd/agentred/service_test.go` |
| Go：平台 ServiceManager 与配置 state | systemd unit / launchd plist / 计划任务参数生成；可注入命令执行器；显式 flag/环境/state/default 优先级；自定义配置切后台不丢失 | 参考 `internal/app/system_test.go` 与 `internal/pkg/agrctlinstall/install_test.go` |
| Go：Windows local IPC | 公共 mux 路由不变；named-pipe endpoint 稳定且不暴露原始路径；Windows round trip；Unix socket 回归；windows/amd64 与 windows/arm64 交叉构建 | 现有 `internal/daemon/daemon_test.go` + 新平台测试 |
| 安装脚本 | 先用本地 HTTP fixture + `AGENTRED_RELEASE_BASE_URL` / `AGENTRED_INSTALL_DIR` 让 checksum 成功、checksum 不匹配拒绝、fallback 安装目录、`--version` 确认自动变红/变绿；PowerShell 在 Windows runner 执行 | 新建 installer fixture/test seam，不再只靠目视 review |
| 前端：加载状态机与三步引导 | loading/error/ready；错误重试且不误显示空态；平台切换更新命令；复制；步骤流转；配对失败保留输入；成功后衔接 Agent 后端 | 现有 `remote-devices/*.test.ts(x)`（vitest + happy-dom + `vi.mock` wails） |
| 前端：i18n | 新键 zh-CN/en 同步、`t("…")` 静态键可解析 | 现有 `frontend/src/__tests__/i18n.test.ts` 守卫 |
| 发布流水线 | source review + 机械检查 agentred 六平台矩阵、依赖关系、资产名、两个 checksum 名、install 脚本与完整 ldflag 路径；不实际发布 Release | 现有 `release.yml` / `nightly.yml` |
| 真实后台服务管理 | 自动测试保护 manifest/命令参数；wrap-up 在 macOS 真机验证 LaunchAgent install/start/status/restart/stop/uninstall；Linux systemd 与 Windows 计划任务由对应 CI runner/接口测试补充，手测仅补充、不替代 RED | — |

## Open questions

（空）
