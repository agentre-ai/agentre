import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, it, expect } from "vitest";

import { AgentSpawnCard, shortenModelName } from "./card";
import type { ChatBlockData } from "@/stores/chat-streams-store";
import enCommon from "@/i18n/locales/en/common.json";
import zhCommon from "@/i18n/locales/zh-CN/common.json";

// expandCard 点击卡片头部展开详情区。折叠态下 agent-spawn-meta-* 节点本就常驻
// DOM(展开动画靠 CSS grid-template-rows,不是 display:none),getByTestId 在
// 折叠时一样查得到——只查 testid 证明不了值真的进了"要展开才可见"的区域。这里
// 改为先点击、断言 aria-hidden 确实从 "true" 翻到 "false",再把查询限定在这个
// 展开容器内,把"展开后可见"这个契约钉住;jsdom 不做布局,像素级可见性验证
// 留给人工复核。
// extractShrinkFactor 读出一个元素 Tailwind flex-shrink 类锁定的因子:
// `shrink-0` → 0(明确不可收缩);`shrink-[N]`(整数或小数) → N;裸 `shrink` →
// 1(默认因子);三种都不命中 → null(该元素完全没有可识别的 shrink 类)。调
// 用方必须自己决定 null 是否算失败——不像之前 `?? "1"` 那样把"找不到"和
// "就是 1"混同,那会让"整个删掉 shrink 类"这种破坏也悄悄通过。模型徽标的
// 让位因子取值 <1(比极简进度更晚让位),所以这里的方括号分支支持小数,不只
// 整数。
function extractShrinkFactor(className: string): number | null {
  if (/(?:^|\s)shrink-0(?:\s|$)/.test(className)) return 0;
  const bracket = /(?:^|\s)shrink-\[([0-9]*\.?[0-9]+)\](?:\s|$)/.exec(
    className,
  );
  if (bracket) return Number(bracket[1]);
  if (/(?:^|\s)shrink(?:\s|$)/.test(className)) return 1;
  return null;
}

function expandCard(container: HTMLElement): HTMLElement {
  const details = container.querySelector('[data-slot="agent-spawn-details"]');
  if (!details) throw new Error("Expected agent-spawn-details container");
  expect(details).toHaveAttribute("aria-hidden", "true");
  fireEvent.click(screen.getByRole("button", { expanded: false }));
  expect(details).toHaveAttribute("aria-hidden", "false");
  return details as HTMLElement;
}

describe("AgentSpawnCard", () => {
  it("renders nothing without canonical", () => {
    const block = { type: "tool_use" } as unknown as ChatBlockData;
    const { container } = render(<AgentSpawnCard toolBlock={block} />);
    expect(container.firstChild).toBeNull();
  });

  it("renders Agent header with description + type", () => {
    const block = {
      type: "tool_use",
      toolName: "Task",
      canonical: {
        kind: "agent.spawn",
        agentSpawn: {
          taskId: "1",
          subagentType: "code-reviewer",
          taskDescription: "review PR",
          toolUses: 3,
          totalTokens: 1200,
          status: "running",
        },
      },
    } as unknown as ChatBlockData;
    render(<AgentSpawnCard toolBlock={block} />);
    expect(screen.getByText("Agent")).toBeDefined();
    expect(screen.getByText("review PR")).toBeDefined();
    expect(screen.getByText("code-reviewer")).toBeDefined();
    // R9: running 态下 toolUses/totalTokens 以无文案的极简形式渲染在头部
    // (状态胶囊左侧),而非旧形态的 "3 tools"/"1.2K tok" 文案。
    expect(screen.getByTestId("agent-spawn-progress").textContent).toBe(
      "3 · 1.2K",
    );
  });

  it("overlays runtime state from block.subagent onto canonical.agentSpawn", () => {
    // canonical.agentSpawn 由 translator 算静态字段(description/subagentType/prompt),
    // 运行时态(toolUses/totalTokens/durationMs/status)来自 SubagentStarted/Progress/
    // Done 事件经 mergeSubagentMeta 合并到 block.subagent;readSpawn 必须把 block.subagent
    // overlay 上去,否则 header chip 永远显示 0 工具 / 无 token。
    const block = {
      type: "tool_use",
      toolName: "Task",
      canonical: {
        kind: "agent.spawn",
        agentSpawn: {
          taskDescription: "review PR",
          subagentType: "code-reviewer",
          prompt: "please review",
        },
      },
      subagent: {
        toolUses: 5,
        totalTokens: 2400,
        status: "completed",
        durationMs: 12000,
      },
    } as unknown as ChatBlockData;
    const { container } = render(<AgentSpawnCard toolBlock={block} />);
    expect(screen.getByText(/DONE/)).toBeDefined();
    // R8: 完整的工具数/tokens/耗时下沉到展开区 meta 行,不再出现在头部;
    // 展开卡片、把查询限定在展开容器内,证明它们确实进了那个区域。
    const details = expandCard(container);
    expect(
      within(details).getByTestId("agent-spawn-meta-tools").textContent,
    ).toBe("5");
    expect(
      within(details).getByTestId("agent-spawn-meta-tokens").textContent,
    ).toContain("2.4K tok");
    expect(
      within(details).getByTestId("agent-spawn-meta-duration").textContent,
    ).toContain("12.0s");
  });

  it("shows DONE when completed", () => {
    const block = {
      type: "tool_use",
      toolName: "Task",
      canonical: {
        kind: "agent.spawn",
        agentSpawn: {
          taskId: "1",
          status: "completed",
        },
      },
    } as unknown as ChatBlockData;
    const result = {
      type: "tool_result",
      text: "summary text",
    } as unknown as ChatBlockData;
    render(<AgentSpawnCard toolBlock={block} resultBlock={result} />);
    expect(screen.getByText("DONE")).toBeDefined();
  });

  it("renders an outlined model badge from the tool-input alias (A1)", () => {
    const block = {
      type: "tool_use",
      toolName: "Task",
      canonical: {
        kind: "agent.spawn",
        agentSpawn: {
          taskId: "1",
          subagentType: "general-purpose",
          model: "haiku",
        },
      },
    } as unknown as ChatBlockData;
    render(<AgentSpawnCard toolBlock={block} />);
    const badge = screen.getByTestId("agent-spawn-model-badge");
    expect(badge.textContent).toBe("haiku");
    // 描边芯片:transparent 底 + border-strong 描边,与实心角色芯片(bg-secondary)区分。
    expect(badge.className).toContain("border-border-strong");
    expect(badge.className).not.toContain("bg-secondary");
  });

  it("overrides the alias with the normalized runtime model once the subagent frame arrives (A2)", () => {
    const block = {
      type: "tool_use",
      toolName: "Task",
      canonical: {
        kind: "agent.spawn",
        agentSpawn: {
          taskId: "1",
          model: "haiku",
        },
      },
      subagent: {
        model: "claude-haiku-4-5-20251001",
      },
    } as unknown as ChatBlockData;
    render(<AgentSpawnCard toolBlock={block} />);
    expect(screen.getByTestId("agent-spawn-model-badge").textContent).toBe(
      "haiku-4-5",
    );
  });

  it("shows only the model badge when subagentType is absent (A3)", () => {
    // 缺失与降级规则 2:subagent_type 缺失但模型存在时,只渲染模型徽标。
    const block = {
      type: "tool_use",
      toolName: "Task",
      canonical: {
        kind: "agent.spawn",
        agentSpawn: { taskId: "1" },
      },
      subagent: { model: "claude-opus-5" },
    } as unknown as ChatBlockData;
    render(<AgentSpawnCard toolBlock={block} />);
    expect(screen.getByTestId("agent-spawn-model-badge").textContent).toBe(
      "opus-5",
    );
    // 角色芯片不应出现在头部——subagentType 缺失时头部唯一渲染的身份徽标是
    // 模型。断言渲染结果本身(角色芯片的 testid 找不到),而非某个样式类是否
    // 出现:换掉角色芯片的底色 token 不该让这条用例继续通过,因为它证明的是
    // "芯片没有被渲染",不是"芯片没有用某个特定类名"。
    expect(screen.queryByTestId("agent-spawn-role-badge")).toBeNull();
  });

  it("renders no badge when neither source has a model (A4)", () => {
    const block = {
      type: "tool_use",
      toolName: "Task",
      canonical: {
        kind: "agent.spawn",
        agentSpawn: { taskId: "1", subagentType: "general-purpose" },
      },
      subagent: { toolUses: 2 },
    } as unknown as ChatBlockData;
    render(<AgentSpawnCard toolBlock={block} />);
    expect(screen.queryByTestId("agent-spawn-model-badge")).toBeNull();
  });

  it("shows STOPPED label when cancelled, not RUNNING/spin", () => {
    // 用户在 turn 内点 Stop 时,chat_svc 把仍 running 的 SubagentStateBlock 改成
    // "canceled" 落 DB。前端必须识别这个值否则 narrowSpawnStatus 会 drop 掉它,
    // 回退到 base.status="running",卡片继续 spin → bug。
    const block = {
      type: "tool_use",
      toolName: "Task",
      canonical: {
        kind: "agent.spawn",
        agentSpawn: {
          taskId: "1",
          taskDescription: "long running task",
          status: "running",
        },
      },
      subagent: {
        status: "canceled",
        durationMs: 4200,
      },
    } as unknown as ChatBlockData;
    render(<AgentSpawnCard toolBlock={block} />);
    expect(screen.getByText(/STOPPED/)).toBeDefined();
    expect(screen.queryByText(/RUNNING/)).toBeNull();
  });

  it("shows a unit-less numeric progress at the status pill's left while running (R9, A10)", () => {
    const block = {
      type: "tool_use",
      toolName: "Task",
      canonical: {
        kind: "agent.spawn",
        agentSpawn: {
          taskId: "1",
          status: "running",
          toolUses: 3,
          totalTokens: 14500,
        },
      },
    } as unknown as ChatBlockData;
    const { container } = render(<AgentSpawnCard toolBlock={block} />);
    const progress = screen.getByTestId("agent-spawn-progress");
    expect(progress.textContent).toBe("3 · 14.5K");
    // 无文案的极简进度对读屏软件是两个无名数字;补的 aria-label 让折叠态下
    // 也能读懂这两个数字分别是什么,不必强制展开卡片。断言只查数值本身,不
    // 绑定具体语言用词,避免与 locale 措辞耦合。
    const progressAriaLabel = progress.getAttribute("aria-label");
    expect(progressAriaLabel).toContain("3");
    expect(progressAriaLabel).toContain("14.5K");
    expect(progress.textContent).not.toMatch(/tok|个工具/);
    // R9: 这个元素被定义为"纯数字、无文案"。aria-label 服务读屏且不可见,
    // 符合 R9；但 title 会在鼠标悬停时可见地弹出同样的文案,等于把 R9 刚
    // 去掉的文案又从后门带回来了,必须不存在。
    expect(progress).not.toHaveAttribute("title");
    // R8/A10: 展开区 meta 行(带标签的完整值)与头部极简进度同时存在——展开
    // 卡片、把查询限定在展开容器内,证明完整值确实也进了那个区域。
    const details = expandCard(container);
    expect(
      within(details).getByTestId("agent-spawn-meta-tools").textContent,
    ).toBe("3");
    expect(
      within(details).getByTestId("agent-spawn-meta-tokens").textContent,
    ).toBe("14.5K tok");
  });

  it("hides the minimal progress once the dispatch is done (R9, A10)", () => {
    const block = {
      type: "tool_use",
      toolName: "Task",
      canonical: {
        kind: "agent.spawn",
        agentSpawn: {
          taskId: "1",
          status: "completed",
          toolUses: 3,
          totalTokens: 14500,
        },
      },
    } as unknown as ChatBlockData;
    render(<AgentSpawnCard toolBlock={block} />);
    expect(screen.queryByTestId("agent-spawn-progress")).toBeNull();
  });

  it("gives the description a much larger shrink priority than the minimal progress, so description gives way first (R11)", () => {
    // jsdom 不做布局,像素级的"谁先被截断"验证留给人工复核(spec 测试接缝已注明)。
    // 这里锁住让位次序背后真正起作用的机制:两者若用同一个收缩因子,会按各自
    // 内容宽度成比例同时收缩,描述短、进度长时进度反而先被挤;要让描述真的先
    // 让位,描述的 flex-shrink 权重必须显著大于进度,不受两者内容长度比例影响。
    const block = {
      type: "tool_use",
      toolName: "Task",
      canonical: {
        kind: "agent.spawn",
        agentSpawn: {
          taskId: "1",
          taskDescription: "x",
          status: "running",
          toolUses: 3,
          totalTokens: 14500,
        },
      },
    } as unknown as ChatBlockData;
    render(<AgentSpawnCard toolBlock={block} />);
    const description = screen.getByText("x");
    const progress = screen.getByTestId("agent-spawn-progress");
    const pill = screen.getByText(/RUNNING/);

    // extractShrinkFactor 显式区分三种情况而非一律 fallback 到某个默认值:
    // 没有任何 shrink 类(改动把它整个删掉)返回 null;`shrink-0`(改动关掉了
    // 可收缩性)返回 0;裸 `shrink` 返回默认因子 1;`shrink-[N]` 返回 N。
    // 之前 `?? "1"` 的写法对"删掉 shrink"和"改成 shrink-0"两种破坏都会
    // 悄悄算出 1,让契约形同虚设——这里让这两种破坏都能被下面的断言钉住。
    const descriptionShrink = extractShrinkFactor(description.className);
    const progressShrink = extractShrinkFactor(progress.className);
    expect(descriptionShrink).not.toBeNull();
    expect(progressShrink).not.toBeNull();
    // 进度必须仍然是"可收缩的"——如果它被改成 shrink-0,这里必须变红,
    // 而不是让下面的倍数比较因为"0 也小于任何正数的四分之一"而continue通过。
    expect(progressShrink).toBeGreaterThan(0);
    expect(descriptionShrink as number).toBeGreaterThan(
      (progressShrink as number) * 4,
    );
    // 状态胶囊在任何宽度下都不参与收缩(R11 后半已成立,这里一并锁住)。
    expect(pill.className).toContain("shrink-0");
  });

  it("hides the minimal progress for canceled and error states, not just completed", () => {
    const canceled = {
      type: "tool_use",
      toolName: "Task",
      canonical: {
        kind: "agent.spawn",
        agentSpawn: { taskId: "1", status: "running", toolUses: 2 },
      },
      subagent: { status: "canceled" },
    } as unknown as ChatBlockData;
    const { unmount } = render(<AgentSpawnCard toolBlock={canceled} />);
    expect(screen.queryByTestId("agent-spawn-progress")).toBeNull();
    unmount();

    const errored = {
      type: "tool_use",
      toolName: "Task",
      canonical: {
        kind: "agent.spawn",
        agentSpawn: { taskId: "1", status: "running", toolUses: 2 },
      },
    } as unknown as ChatBlockData;
    const result = {
      type: "tool_result",
      isError: true,
      text: "boom",
    } as unknown as ChatBlockData;
    render(<AgentSpawnCard toolBlock={errored} resultBlock={result} />);
    expect(screen.queryByTestId("agent-spawn-progress")).toBeNull();
  });

  it("shows the uncut model value in the expanded meta row, not the shortened badge form (R8)", () => {
    const block = {
      type: "tool_use",
      toolName: "Task",
      canonical: {
        kind: "agent.spawn",
        agentSpawn: { taskId: "1", model: "haiku" },
      },
      subagent: { model: "claude-haiku-4-5-20251001" },
    } as unknown as ChatBlockData;
    const { container } = render(<AgentSpawnCard toolBlock={block} />);
    expect(screen.getByTestId("agent-spawn-model-badge").textContent).toBe(
      "haiku-4-5",
    );
    const details = expandCard(container);
    expect(
      within(details).getByTestId("agent-spawn-meta-model").textContent,
    ).toBe("claude-haiku-4-5-20251001");
  });

  it("omits zero-value items from the meta row instead of showing a 0 placeholder (A12)", () => {
    const block = {
      type: "tool_use",
      toolName: "Task",
      canonical: {
        kind: "agent.spawn",
        agentSpawn: {
          taskId: "1",
          status: "completed",
          toolUses: 4,
          totalTokens: 0,
        },
      },
    } as unknown as ChatBlockData;
    const { container } = render(<AgentSpawnCard toolBlock={block} />);
    const details = expandCard(container);
    expect(
      within(details).getByTestId("agent-spawn-meta-tools").textContent,
    ).toBe("4");
    expect(within(details).queryByTestId("agent-spawn-meta-tokens")).toBeNull();
    expect(
      within(details).queryByTestId("agent-spawn-meta-duration"),
    ).toBeNull();
    expect(within(details).queryByTestId("agent-spawn-meta-model")).toBeNull();
    expect(within(details).queryByText(/0 tok/)).toBeNull();
  });

  it("shows the model badge's full text without a width-independent cap when space is not the constraint (R7)", () => {
    // 修订后的 R7:横向空间充足时徽标必须完整显示裁剪后的全文,不做与可用
    // 宽度无关的固定截断(此前 max-w-[16ch] 无条件生效,720px 下也在约 16
    // 字符处切掉)。实现决策 9 仍然成立——网关接入的第三方模型名是任意串,
    // 可以很长——但"可以很长"应该靠 R11 的 flex-shrink 让位在空间不足时才
    // 裁,而不是一个跟宽度无关的硬 max-w。这里断言:不存在这种硬上限类。
    const longModel = "moonshotai/kimi-k2-0905-preview";
    const block = {
      type: "tool_use",
      toolName: "Task",
      canonical: {
        kind: "agent.spawn",
        agentSpawn: { taskId: "1" },
      },
      subagent: { model: longModel },
    } as unknown as ChatBlockData;
    render(<AgentSpawnCard toolBlock={block} />);
    const badge = screen.getByTestId("agent-spawn-model-badge");
    // 完整原值仍在 DOM 里(截断只是视觉裁切,不是数据丢失)。
    expect(badge.textContent).toBe(longModel);
    expect(badge.className).not.toMatch(/max-w-\[/);
  });

  it("puts truncate on an inner span and overflow-hidden on the flex item, so the ellipsis actually renders (R7)", () => {
    // 上一轮的测试只断言 className 里出现过 "truncate" 这个词,抓不到
    // "truncate 加在 inline-flex 元素上不生效"这个缺陷——text-overflow:
    // ellipsis 只作用于块级容器,加在 flex 容器本身,文本子节点会变成匿名
    // flex item,省略号永远不会被绘制,实际效果是齐字硬切、没有任何"后面还
    // 有内容"的提示。同文件的 agent-spawn-progress 元素做对了(truncate 在
    // 内层 span、overflow-hidden 留在 flex 容器):这里断言模型徽标照同一
    // 结构写,而不是只查字符串里是否出现过这个类名。
    const longModel = "moonshotai/kimi-k2-0905-preview";
    const block = {
      type: "tool_use",
      toolName: "Task",
      canonical: {
        kind: "agent.spawn",
        agentSpawn: { taskId: "1" },
      },
      subagent: { model: longModel },
    } as unknown as ChatBlockData;
    render(<AgentSpawnCard toolBlock={block} />);
    const badge = screen.getByTestId("agent-spawn-model-badge");
    // 承载 truncate 的必须是内层子节点,不是这个 flex item 本身。
    expect(badge.className).not.toMatch(/(?:^|\s)truncate(?:\s|$)/);
    // flex item 自己留 overflow-hidden + min-w-0,让它在挤压下真能收缩到 0
    // 而不是撑破容器或被内容撑宽。
    expect(badge.className).toMatch(/(?:^|\s)overflow-hidden(?:\s|$)/);
    expect(badge.className).toMatch(/(?:^|\s)min-w-0(?:\s|$)/);
    const innerSpan = badge.querySelector("span");
    expect(innerSpan).not.toBeNull();
    expect(innerSpan!.className).toMatch(/(?:^|\s)truncate(?:\s|$)/);
    expect(innerSpan!.textContent).toBe(longModel);
  });

  it("gives the model badge a smaller shrink priority than the minimal progress, so it is the last to yield (R7, R11)", () => {
    // R11 的让位次序是"任务描述 → 极简进度 → 其它",模型徽标属于"其它":
    // 排在描述与极简进度都已经让位之后才开始收缩。用同样的机制(flex-shrink
    // 权重比较)锁住这个次序,而不是像描述/进度那样只比较两者——这里额外
    // 断言模型徽标的权重明显小于极简进度的默认权重(1)。
    const block = {
      type: "tool_use",
      toolName: "Task",
      canonical: {
        kind: "agent.spawn",
        agentSpawn: {
          taskId: "1",
          status: "running",
          toolUses: 3,
          totalTokens: 14500,
        },
      },
      subagent: { model: "claude-haiku-4-5-20251001" },
    } as unknown as ChatBlockData;
    render(<AgentSpawnCard toolBlock={block} />);
    const badge = screen.getByTestId("agent-spawn-model-badge");
    const progress = screen.getByTestId("agent-spawn-progress");
    const modelShrink = extractShrinkFactor(badge.className);
    const progressShrink = extractShrinkFactor(progress.className);
    expect(modelShrink).not.toBeNull();
    expect(progressShrink).not.toBeNull();
    // 必须仍然可收缩(不是 shrink-0,否则空间不足时不会真的参与让位)。
    expect(modelShrink as number).toBeGreaterThan(0);
    expect(progressShrink as number).toBeGreaterThan(
      (modelShrink as number) * 2,
    );
  });

  it("renders no meta row at all when every dispatch metric is empty", () => {
    const block = {
      type: "tool_use",
      toolName: "Task",
      canonical: {
        kind: "agent.spawn",
        agentSpawn: { taskId: "1", subagentType: "general-purpose" },
      },
    } as unknown as ChatBlockData;
    const { container } = render(<AgentSpawnCard toolBlock={block} />);
    const details = expandCard(container);
    expect(within(details).queryByTestId("agent-spawn-meta-row")).toBeNull();
  });
});

describe("shortenModelName", () => {
  it("strips the claude- prefix and the trailing 8-digit date segment", () => {
    expect(shortenModelName("claude-haiku-4-5-20251001")).toBe("haiku-4-5");
  });

  it("strips the claude- prefix when there is no trailing date segment", () => {
    expect(shortenModelName("claude-opus-5")).toBe("opus-5");
  });

  it("leaves a bare alias untouched", () => {
    expect(shortenModelName("sonnet")).toBe("sonnet");
  });

  it("leaves an arbitrary third-party model name untouched", () => {
    expect(shortenModelName("glm-4.6")).toBe("glm-4.6");
  });
});

describe("progress accessibility label i18n (canonical.agentSpawn.progressAria)", () => {
  it("announces the singular English wording when there is exactly one tool, not '1 tools' (i18next plural fallback)", () => {
    // progressAria.tools 之前只有一个不带 _one/_other 后缀的裸键
    // ("{{count}} tools")。i18next 在有 count 选项时会先找 `_other`,该键
    // 没有任何后缀变体时才整体回退到裸键——所以即使 toolUses === 1(首帧
    // 进度的常见状态),读到的也是裸键 "{{count}} tools" → "1 tools"。
    const block = {
      type: "tool_use",
      toolName: "Task",
      canonical: {
        kind: "agent.spawn",
        agentSpawn: {
          taskId: "1",
          status: "running",
          toolUses: 1,
          totalTokens: 0,
        },
      },
    } as unknown as ChatBlockData;
    render(<AgentSpawnCard toolBlock={block} />);
    const progress = screen.getByTestId("agent-spawn-progress");
    const ariaLabel = progress.getAttribute("aria-label");
    expect(ariaLabel).toBe("1 tool");
    expect(ariaLabel).not.toBe("1 tools");
  });

  it("avoids the reserved i18next `count` interpolation option for the tokens template", () => {
    // formatTokenCount 产出的是展示用字符串(如 "14.5K"),不是可数的语法数
    // 量。card.tsx 把它塞进了 i18next 的保留选项 `count`——该选项会被送进
    // Intl.PluralRules.select() 做单复数判定,当前只是靠 NaN 落到 "other"
    // 侥幸没炸。模板不应该依赖这个保留名字,这里锁住两份 locale 都已经改用
    // 普通具名占位符,不再含 "{{count}}"。
    expect(enCommon.canonical.agentSpawn.progressAria.tokens).not.toContain(
      "{{count}}",
    );
    expect(zhCommon.canonical.agentSpawn.progressAria.tokens).not.toContain(
      "{{count}}",
    );
  });

  it("still substitutes the formatted token value correctly after moving off the reserved `count` option", () => {
    // 配套回归:locale 模板换了占位符名之后,card.tsx 调用点必须传同名参数,
    // 否则插值对不上,渲染结果会原样留下 "{{value}}" 这种字面量。
    const block = {
      type: "tool_use",
      toolName: "Task",
      canonical: {
        kind: "agent.spawn",
        agentSpawn: {
          taskId: "1",
          status: "running",
          toolUses: 0,
          totalTokens: 2400,
        },
      },
    } as unknown as ChatBlockData;
    render(<AgentSpawnCard toolBlock={block} />);
    const progress = screen.getByTestId("agent-spawn-progress");
    const ariaLabel = progress.getAttribute("aria-label");
    expect(ariaLabel).toBe("2.4K tokens");
    expect(ariaLabel).not.toMatch(/\{\{/);
  });
});
