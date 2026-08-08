# Pi Agent 零宽限期进程树终止

> Status: Approved
> Owner: Agentre maintainers
> Last updated: 2026-08-08

**Objective:** Pi Agent 已接受的流在取消并完成有界结算后，必须可靠终止整棵进程树，使 Linux CI 与实际运行都不留下继续执行的工具子进程。

**Hard invariant:** 取消后的锚点结算窗口、错误分类、`Close` 的有界与幂等语义不得回退。

## Problem

1. **零宽限期终止存在组长先退出的竞态。** GitHub Actions 的 Go Test 作业在 `TestCanceledAcceptedStreamSettlesAnchorBeforeTerminatingProcessTree` 中报告工具进程在一秒终止边界后仍存活。当前零宽限期路径先关闭 stdin；RPC shell 可因 EOF 退出并被 `Wait` 回收，随后再按组长 PID 杀进程组时已无法定位仍存活的工具子进程。证据见失败作业与 `pkg/piagent` 的终止顺序。
2. **现有真实进程测试只能间歇命中该竞态。** 同一测试在相邻 Linux CI 运行中既有成功也有失败；增加重试或延长等待只会掩盖调度竞态，不能保证工具工作已停止。

## Actors and user stories

1. As a 取消 Pi Agent 运行的调用方, I want 结算结束后整棵进程树在既有边界内终止, so that 已取消的工具不会继续执行。
2. As a 维护者, I want 该竞态由确定性的单元测试保护, so that CI 不再依赖进程调度时序碰巧通过。

## Design decisions

| # | Decision | Basis and rejected option |
|---|---|---|
| 1 | 零宽限期终止必须在关闭 stdin 可能使组长退出之前，对仍可寻址的进程树执行强制终止，然后再完成 writer 关闭与进程回收。 | 进程组只有在组长仍可查询时才能由当前安全检查可靠定位。Rejected: 提高测试超时或重试——不能阻止真实工具继续运行。 |
| 2 | 保留正宽限期的现有优雅中断契约，本轮只修复取消结算使用的零宽限期分支。 | 失败证据来自 `terminate(..., 0)`；扩大到所有关闭策略会引入未经要求的生命周期变化。Rejected: 重写完整进程管理——超出本次 CI 修复范围。 |
| 3 | 用可控进程句柄确定性验证“强制终止先于 stdin 关闭”，并继续保留真实 Unix 进程树测试验证外部结果。 | 前者稳定复现根因，后者覆盖 OS 进程树集成。Rejected: 仅弱化 `kill -0` 存活断言——会失去对真实残留工具进程的保护。 |

## Termination flow

当已接受的 Pi Agent 流因调用方取消而结束结算窗口，且终止宽限期为零时，运行时先向进程树发送既有中断信号，随后在组长仍可寻址时立即强制终止整棵树。只有完成该动作后，运行时才关闭 stdin、释放可能阻塞的 writer，并等待进程结果。

调用方继续观察到既有的流错误、已捕获的用户锚点以及有界的 `Close`。重复 `Close` 仍返回同一终态，不再次启动终止流程。强制终止或进程回收产生的错误继续使用现有的规范化边界，不暴露原始命令或敏感 stderr。

当宽限期大于零时，现有“先中断、等待宽限期、必要时强杀”的行为保持不变。

## Out of scope

- 重构所有正宽限期进程退出与进程组复用策略。
- 修改 Windows 进程树实现。
- 增加 CI 重试、提高进程消失超时或删除真实进程树回归测试。
- 修改 Pi Agent 的结算、锚点捕获或错误文案。

## Testing decisions

| Seam | What it verifies | Prior art |
|---|---|---|
| `rpcProcess` 零宽限期终止边界 | stdin 关闭会使组长立即退出时，整树强杀仍必须先发生；恢复旧顺序会稳定变红 | `pkg/piagent/stream_close_test.go` 的关闭与阻塞 writer 测试 |
| 真实 Unix Pi RPC shell 与后台工具进程 | 取消后锚点先结算，父进程和工具进程都在既有边界内消失，`Close` 保持有界与幂等 | `TestCanceledAcceptedStreamSettlesAnchorBeforeTerminatingProcessTree` |
| `pkg/piagent` 与后端总套件 | 修复不改变其他流关闭、错误分类和跨包调用契约 | 现有 Go 单元测试与 CI Go Test 作业 |

该行为可在自动化测试边界完整观察，无需额外手工 UI 验证。

## Relevant links

- [失败的 GitHub Actions 作业](https://github.com/agentre-hub/agentre/actions/runs/31238403956/job/93055076493)
- 引入相关取消结算与真实进程树测试的提交：`ba4bd21a652b29873b8ebc3c4a767645a4858106`

## Open questions

None.
