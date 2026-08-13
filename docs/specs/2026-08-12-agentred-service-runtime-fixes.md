# agentred 后台服务与验收隔离修复

<!-- File: docs/specs/2026-08-12-agentred-service-runtime-fixes.md -->

> Status: Approved
> Owner: cmd/agentred + desktop e2e/bootstrap
> Last updated: 2026-08-12

**Objective:** 修复 macOS 用户级 agentred 服务启动/重启不可靠与桌面 e2e keychain 装配分叉，使 reviewed agentred 在真实 macOS、Linux 用户级服务环境中都能完成生命周期，并使真实桌面配对后的 watcher 稳定在线。

**Hard invariant:** Linux `systemd --user` 已通过的生命周期、agentred 持久化运行配置、生产 system keychain、6 位一次性配对码、TOFU 指纹、设备 token、现有 Wails 配对流程和 watcher 在线状态协议不得回归；e2e 运行不得读取、覆盖或删除任何生产 system keychain 条目。

## Problem

1. **macOS LaunchAgent 的启动结果存在竞态，`install --start` 会在 daemon 尚未就绪时返回错误状态。** 在 macOS 14.8.7 arm64 上，reviewed HEAD `afe73523` 的真实运行中，`agentred service install --start` 首先输出 `Daemon stopped`；稍后 `launchctl` 显示进程 running，但本地状态 socket 尚不存在。当前 `Start` 在 `bootstrap` 后只做一次 `launchctl print`，没有等待 daemon 与本地 IPC 就绪（`cmd/agentred/service_launchd.go`）。
2. **macOS LaunchAgent 的 restart 使用 `bootout → bootstrap`，会撞上 launchd 注销竞态。** 同一真实运行中，`agentred service restart` 返回 `Bootstrap failed: 5: Input/output error`。当前 `Restart` 无条件先 `bootout` 再立即 `bootstrap`，而 launchd 提供了对已加载 job 的原生 kickstart/restart 路径。
3. **e2e keychain 覆盖发生得太晚，Remote Device Add 与 watcher 可能使用不同后端。** `bootstrap.Init` 先执行 `InitServer` 与 `InitRemoteDevice`，令 Remote Device 服务捕获 system keychain；随后 `main.go` 才调用 `fakes.Install`，其中 `installE2EKeychainOverride` 把全局默认值改为 file keychain。真实验收因此出现 Add 写 system keychain、watcher 读 file keychain并报 `unauthorized` 的假阴性。
4. **上述 e2e 分叉会污染真实凭据。** 隔离 e2e 数据库从设备 ID 1 开始，而 token account 名只含设备 ID；验收曾覆盖真实 `agentre-daemon-token-1`。这违反 e2e 隔离边界，即使测试数据库、端口与文件目录均已隔离也不安全。
5. **平台生命周期不能只由 mock、交叉编译或接口测试宣称成功。** 当前 Linux 在 `coding.local` Debian 12 amd64 上已真实通过，macOS 未通过。用户要求本修正轮交付前至少在可用的真实 macOS 与 Linux 设备上分别跑通。

## Actors and user stories

1. 作为 macOS 用户，我希望 `service install --start` 和 `service restart` 只在 daemon 真正可用后报告成功，以便关闭终端后仍能稳定使用远端设备。
2. 作为 Linux 用户，我希望修复 macOS 时不破坏已经工作的 `systemd --user` 生命周期与持久化配置。
3. 作为维护者，我希望 e2e 配对从进程初始化开始就使用隔离 keychain，以便验收既能观察真实 Wails/daemon 行为，又不会触碰我的生产凭据。
4. 作为发布负责人，我希望 macOS 与 Linux 都留下真机命令、退出码、监听状态与清理证据，以便平台支持结论可复现。

## Design decisions

| # | Decision | Basis and rejected option |
|---|---|---|
| 1 | macOS 对已加载 LaunchAgent 使用 launchd 原生启动/重启操作；只有未加载 job 才执行 bootstrap。 | 避免 `bootout → bootstrap` 的异步注销竞态，同时保留 LaunchAgent 用户级边界。Rejected: 在 bootout 后盲加固定 sleep — 依赖机器速度，仍可能偶发失败。 |
| 2 | start/restart 成功必须经过有界就绪等待：launchd job 为 running，且 agentred 本地状态端点可读取；超时返回非零并保留可执行诊断信息。 | “launchd 有进程”不等于 daemon 已可用；原故障正是两者时间窗口不同。Rejected: 继续只检查一次 `launchctl print` — 会误报 stopped 或提前成功。 |
| 3 | stop/uninstall 继续使用 bootout，但操作完成后必须观察 job 已卸载/停止；missing service 保持幂等成功。 | 停止与卸载需要明确终态，且不能把正常的 missing service 当故障。Rejected: 所有动作都改为 kickstart — kickstart 不负责卸载。 |
| 4 | `AGENTRE_E2E_KEYCHAIN_DIR` 在 bootstrap 创建任何依赖 keychain 的服务之前生效；同一进程内 Server、Remote Device、ConnPool、watcher 与 e2e seeding 必须捕获同一个 file keychain。 | 从生产者边界消除 split-brain，而不是在 watcher 处增加缺 token fallback。Rejected: 配对后再次重装 Remote Device 服务 — 已创建对象仍可能持有旧实例，且掩盖初始化顺序错误。 |
| 5 | e2e keychain 隔离失败时启动直接失败，不得静默退回 system keychain。 | silent fallback 会再次污染真实凭据。Rejected: 只给 e2e 设备 ID 加大偏移 — 仍然触碰生产 keychain，不能构成安全隔离。 |
| 6 | 本轮以真实 macOS + `opsctl local-coding` Linux 双平台运行作为交付硬门；两端都必须使用 reviewed HEAD 构建并完成清理。 | 用户明确要求至少跑通两种现有真实设备。Rejected: Linux 只跑单测或交叉构建 — 不能观察 systemd、linger、进程与 socket。 |

## macOS LaunchAgent 生命周期

当用户已通过前台 `agentred run` 保存 host、port、TLS、Relay server 或自定义数据目录后，执行 `agentred service install --start` 必须安装/更新当前用户的 LaunchAgent，并等待同一数据目录下的 daemon 本地状态可读。成功输出首行必须为 `Daemon running`，后续 `agentred service status` 必须显示注入的版本、持久化监听地址与可读本地状态，不得出现 `Local status unavailable`。

当已安装的 LaunchAgent 正在运行时，执行 `agentred service restart` 必须通过 launchd 的已加载 job 重启路径完成，不得先注销再立即注册。命令成功返回时，新 daemon PID 已建立、本地状态可读、原持久化配置不变。若 job 文件存在但尚未加载，start/restart 可以重新 bootstrap；若 launchd 或 daemon 在有界等待内未就绪，命令退出码非 0，错误包含失败阶段、launchctl 目标以及用户可直接执行的诊断/恢复命令。

`service stop` 成功后必须显示 `Daemon stopped`，job 不再运行且本地 socket/监听端口不可用；`service start` 能从这一状态恢复到可读本地状态。`service uninstall` 必须停止 job、移除 plist，并保持重复执行幂等。所有命令仍为当前用户 LaunchAgent，不请求管理员权限或创建系统级 daemon。

## Linux systemd 回归边界

Linux 继续使用 `systemd --user` 与用户级 unit。修复共享就绪逻辑后，`install --start`、status、restart、stop、start、uninstall 的稳定首行和退出码保持不变。前台显式保存的 host/port/TLS/Relay server 与解析后的 `AGENTRED_DATA_DIR` 必须由后台 unit 恢复。

若 `loginctl enable-linger` 被策略拒绝，install 仍安装 unit，并输出当前已有的明确修复命令；若 linger 已启用，完整生命周期不得出现额外警告。不得触碰同机已有的其他 agentred 二进制、数据目录、进程或端口；真机验收使用唯一 `/tmp` 路径与非默认端口。

## E2E keychain 初始化与安全边界

带 `e2e` build tag 且设置 `AGENTRE_E2E_KEYCHAIN_DIR` 时，应用必须在 bootstrap 装配 Server 和 Remote Device 之前建立 file keychain。目录必须是隔离、当前用户可写的 0700 目录，secret 文件使用 0600。Server、Remote Device Add、ConnPool、watcher 和 e2e login seed 读取到同一实例/同一目录。

如果变量存在但目录缺失、权限不安全或无法初始化，e2e 应在启动阶段以明确错误终止；不得回退 `NewSystem()`。未设置该变量的生产构建继续使用平台 system keychain，行为不变。

一次真实 e2e 配对必须把 device fingerprint 与设备 token 写入隔离目录，watcher 使用同一凭据后变为 online。运行前后检查生产 system keychain，不得新增、修改或删除 `agentre-daemon-token-*` 或 `agentre-device-fingerprint`。验收脚本也不得依靠高位设备 ID 规避碰撞；隔离必须由 keychain backend 边界保证。

## Failure handling and cleanup

- launchctl/systemctl 命令失败必须保留原始原因，并输出可直接执行的诊断命令；不得把权限错误当作 stopped。
- 就绪等待必须有上限并尊重 context 取消，不能无限阻塞 CLI。
- daemon 在等待期间退出时，返回失败而不是继续轮询到一个模糊 timeout。
- 真机验收只创建本轮命名的临时二进制、数据目录、plist/unit 和端口；结束时无论成功失败都执行 stop/uninstall 与残留检查。
- 用户已授权停止的旧 `/tmp/agentred-pr20 run` 保持停止；本轮不得删除其二进制或数据，除非用户另行授权。

## Out of scope

- Windows named pipe、计划任务和 PowerShell installer 的新增修复；现有单元测试与交叉构建不得回归。
- 账号服务器登录、版本登记与旧 daemon 兼容性的新增实现。
- Remote Devices 初始加载错误 UI 的新注入接口或行为变更。
- Relay/TLS/provider/account 功能扩展。
- 恢复已在先前验收中被覆盖且无法取回的旧 `agentre-daemon-token-1`；该真实设备只能通过新配对码重新配对。

## Testing decisions

| Seam | What it verifies | Prior art |
|---|---|---|
| launchd manager unit tests | loaded/unloaded 分支；start/restart 不再无条件 bootout；running 与 missing/permission failure 分类；有界轮询的成功、timeout、context cancel | `cmd/agentred/service_launchd_test.go` |
| service command tests | install --start、start、restart 只在本地状态可读后成功；失败输出稳定且非零；Linux/macOS 共用就绪边界不改变首行 | `cmd/agentred/service_test.go` |
| bootstrap/e2e keychain tests | 设置 e2e dir 时，keychain 在 Remote Device 装配前确定；Add 与 watcher 读写同一 file backend；初始化失败不回退 system keychain | `e2e/fakes/keychain_test.go`、`internal/bootstrap/*_test.go` |
| focused Go tests + full suite | launchd/systemd/service/bootstrap 回归；Windows 构建不回归；`GOWORK=off make check` 全绿 | 现有 Makefile 与 package tests |
| 真实 macOS 运行 | reviewed HEAD 构建在隔离目录完成前台配置 → install --start → status → restart → stop → start → uninstall；每步记录退出码、PID/socket/端口/plist，且最终无残留 | `docs/verification.md` |
| 真实 Linux 运行 | 通过 `opsctl local-coding` 在 Debian 12 amd64 完成同一生命周期，检查 systemd user unit、linger、状态、监听地址和清理 | `docs/verification.md` |
| 真实生产 Wails → Linux daemon | 非 mock pairing code、Wails Add、隔离 keychain、watcher `1 paired · 1 online`、Agent Backends 跳转；运行前后证明 system keychain 未改变 | `e2e/README.md` scratch harness conventions |

自动测试不能替代两台真实设备的生命周期观察。交付验收必须同时满足：macOS 生命周期 holds、Linux 生命周期 holds、生产 Wails 到 Linux daemon 配对后 watcher online、system keychain 无新增/修改/删除、所有本轮服务/进程/端口/临时目录均已清理。任一项失败则 verification 保持 reported/does not hold，不得自动接受。

## Open questions

（空）
