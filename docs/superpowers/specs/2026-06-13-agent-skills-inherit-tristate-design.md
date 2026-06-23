# Agent 技能(plugin)继承 + 双向覆盖 —— 设计

- 日期:2026-06-13
- 范围:组织架构 → agent 详情「技能」区(claudecode 后端)
- 关系:**修订并推翻** [`2026-06-12-agent-skills-tools-design.md`](./2026-06-12-agent-skills-tools-design.md) 中「约束子集 / 默认拒绝」的注入决策(见 §2)。其余(授权单元=plugin、per-agent、`CapSkills` 门控、`--settings` 注入接缝)沿用不变。
- UI 稿:`~/Desktop/agentry.pen` 帧「⑦ Agent 能力区 v3 · 技能继承+双向覆盖」(已确认方向,见 [[reference_agentry_pen_design_file]])。

## 1. 背景与问题

当前(已合并 `d631157`)的技能授权是**默认拒绝(白名单)**模型:

- 注入逻辑 `skill_svc.EnabledPluginsMap` 把**全部已安装** plugin 注入 `--settings enabledPlugins`,授予的 `true`、**未授予的一律 `false`**(`internal/service/skill_svc/skill.go:114-133`)。
- 后果:任何 claudecode agent 在每轮都会**强制关掉**用户 `~/.claude` 里全局开着、但没在 agentre 里授予的插件。等于"偷偷把全局插件全关了,只留授予的那几个"。
- UI:弹窗"取消勾选/移除" = 运行时禁用;实体 `AgentSkillItem{ID, Enabled}` 的 `Enabled` 永远只写 `true`,`false` 从未被前端产生。

用户的两个诉求:

1. **skill vs plugin 是否区分** —— 答:授权单元是 **Claude Code plugin(skill-pack)**,不是单个 skill。CLI `--settings enabledPlugins` 只能按 plugin 开关;plugin 内的单个 skill 仅作展示(数量 badge / `contents`)。UI 文案需理顺,标明"以插件为单位",并把包内 skill 数可视化。
2. **给已开启的加 disable 勾** —— 经 §3 实测确认 `false` 可安全地按 agent 关掉全局插件,故采用**双向三态**(见 §2),满足该诉求。

关键澄清:**per-agent 的 `--settings enabledPlugins` 是进程级、launch-time 覆盖,只作用于该次 spawn 的 claude 子进程,绝不写回 / 影响 `~/.claude` 全局配置。**(§3 已实测验证。)

## 2. 目标模型:继承全局 + 按 agent 双向三态覆盖

每个插件对某 agent 有三种状态。实体 `AgentSkillItem{ID, Enabled}` 天然够用:

| 状态 | `skills_json` 表示 | UI(分段控件) | 注入 |
| --- | --- | --- | --- |
| **继承(默认)** | 不在列表 | 「继承」高亮(中性) | 不注入该 id → CLI 沿用全局 |
| **强制开** | 在列表,`Enabled:true` | 「开」高亮(蓝) | 注入 `{id: true}` |
| **强制关** | 在列表,`Enabled:false` | 「关」高亮(红) | 注入 `{id: false}` |

- 默认(无任何配置)agent = **完全继承** `~/.claude` 全局已启用集,等同裸跑 `claude`。
- 这**推翻**了 `2026-06-12` spec「保留 false 项以约束子集 / 默认拒绝」决策。

### 行为变更(需用户在 spec review 时确认)

改动后,**现有 agent 从"只有授予的插件"变为"继承全局开的 + 强制开的额外开 − 强制关的"**。这是"继承"的应有之义。无需数据迁移(`skills_json` 结构不变,旧的 `{id,true}` 授予项语义自然变为"强制开")。

## 3. CLI 语义 spike —— 已实测确认(claude 2.1.177,2026-06-13)

整个模型依赖"`--settings enabledPlugins` 是合并而非整体替换"。已实测钉死,**结论:成立,且纯进程级、不写回全局**。观测法:`claude -p "hi" --output-format stream-json --verbose [--settings <json>]`,读首个 `system/init` 事件的 `skills` / `slash_commands` / `plugins` 字段。

| 测试 | 传入 `enabledPlugins` | 观测结果 |
| --- | --- | --- |
| 关全局开着的 | `{"superpowers@claude-plugins-official": false}` | superpowers 的 14 个 skill + 全部 slash_command **消失**;superpowers 从 `plugins` 列表移除;**其余插件(code-review/frontend-design/gopls-lsp/opsctl/...)全部保留** |
| 开全局关着的 | `{"ui-ux-pro-max@ui-ux-pro-max-skill": true}` | `ui-ux-pro-max:ui-ux-pro-max` skill + command **出现**;`plugins` 7→8 |
| 是否写回全局 | 上述跑完后 `claude plugin list --json` | superpowers 仍 `enabled:true`、ui-ux 仍 `enabled:false` —— **全局纹丝不动** |

→ 合并语义确认,**§(旧)整体替换回退方案已删除,不需要**。

## 4. 后端改动

### 4.1 发现层透出全局 enabled
- `agentskill.SkillPack` 增 `GloballyEnabled bool`(`internal/pkg/agentskill/agentskill.go`)。
- `claudeskill.parsePluginList` 把 `rawPlugin.Enabled`(已在解析、目前丢弃)填入 `SkillPack.GloballyEnabled`(`internal/pkg/agentskill/claudeskill/discover.go:65-77`);更新该处"当前发现不消费"的注释。`scope` 的取舍:默认 `enabled==true` 即视为全局已开(spike 中各 scope 的 `enabled` 已是有效值,无需按 scope 过滤)。

### 4.2 注入语义翻转(核心)
`skill_svc.EnabledPluginsMap`(`skill.go:114-133`)从"全部已安装→含 false"改为"仅 agent 的显式覆盖→原样 true/false":

```go
// EnabledPluginsMap 返回该 agent 的显式覆盖(强制开=true / 强制关=false)。
// 其余(含全局已开但未覆盖)不出现在 map → CLI 沿用全局 enabledPlugins,实现继承。
func (s *Service) EnabledPluginsMap(ctx context.Context, agentID int64) (map[string]bool, error) {
    a, err := s.agent.Find(ctx, agentID)
    if err != nil || a == nil {
        return nil, err
    }
    out := map[string]bool{}
    for _, it := range a.GetSkills() { // []AgentSkillItem{ID, Enabled}
        out[it.ID] = it.Enabled
    }
    return out, nil
}
```

- 不再依赖 `discover`(注入路径少一次 `claude plugin list`)。stale/未安装的覆盖项原样下发,CLI 自行忽略(无害)。
- `buildSkillsSettings`(`runtimes/claudecode/skills.go`)、`turn_skills` 接缝、`RunRequest.EnabledPlugins` **不变**:非空 → 折进 `--settings`;空(无覆盖)→ 完全继承全局。

### 4.3 目录 DTO 透出全局态
- `SkillPackDTO` 增 `GloballyEnabled bool`(`skill_svc/types.go`)。`merge` 复制整 pack 自然透传(recommended-only 项 `GloballyEnabled=false`)。
- `Enabled` 字段保留语义 = **该 agent 强制开授予**(来自 `GetEnabledPackIDs`),用于目录中"强制开"标识;**三态判定以前端本地 `skills` 数组为准**(present+true=开 / present+false=关 / absent=继承),后端只需多给 `GloballyEnabled`。

## 5. 前端改动

「技能」状态的本地真值 = `org-detail-agent.tsx` 的 `skills: AgentSkillDTO[]`(`{id, enabled}`)。三态映射:**不在数组=继承 / 在数组 true=强制开 / 在数组 false=强制关**。保存仍走底部统一 `UpdateAgent`,`skills` 原样回传(实体不变)。

### 5.1 目录映射(`org/skill-catalog.ts`)
`skillPacksToCatalog` 输入加 `globallyEnabled`,产出:
- 分组:`globallyEnabled` → 「继承(全局已开)」组;`installed && !globallyEnabled` → 「可启用(已安装·全局未开)」组;未装 → 「推荐/可安装」组(`disabledReason=需安装`,沿用)。
- 每行描述拼接全局态(「全局已启用/未启用」)+ 包内 skill 数(`contents.length`)。

### 5.2 三态分段控件(新增通用件,`capability/`)
- 新增 `TriStateToggle`(继承|开|关)纯展示组件:入参 `value: "inherit"|"on"|"off"` + `onChange`。active 着色:继承=中性、开=主色、关=红(`status` 色)。
- `CapabilityPicker` 的行尾从单 checkbox 换成 `TriStateToggle`;`onToggle(id)` 改为 `onSetState(id, next)`。未安装行禁用(沿用 `disabledReason`)。
- `org-detail-agent.tsx` 计算每行 `value`:查本地 `skills` 数组(present? enabled? : inherit);`onSetState`:开→upsert `{id,true}`、关→upsert `{id,false}`、继承→从数组移除。

### 5.3 芯片区三态(`GrantedChips` 扩展)
- `GrantedChip` 增 `tone: "inherit"|"on"|"off"` + `locked?`。样式:继承=灰·`继承`小标·**无 X**;强制开=主色·可 X(→继承);强制关=红·删除线·可 X(→继承)。
- 芯片集合 = **继承**(globallyEnabled 且未被强制关的,灰锁)+ **强制开**(本地 true)+ **强制关**(本地 false,含对全局开插件的关)。计数:`继承 N · 强制开 N · 强制关 N`。
- **加载时机**:`useSkillCatalog` 在技能区可见(`caps.has("skills")`)时**挂载即 `load(false)`**(不再仅弹窗打开才拉),以便芯片区直接显示继承集。代价:打开 claudecode agent 详情多一次 `claude plugin list --json`(软降级、可接受)。加载中先按本地 `skills` 显示强制开/关,加载完补继承芯片。`load(true)` 仍用于弹窗"重扫"。

### 5.4 skill vs plugin 文案理顺(Q1)
- 弹窗副标题:「一个包(plugin)= 一组 skill · 继承全局,按此 agent 覆盖」;每行 `N skills` 数量。不做 skill 级开关(CLI 不支持)。

## 6. i18n(`zh-CN` + `en` 同步,过 `i18n.test.ts`)

- `capability.triState.{inherit,on,off}`(继承 / 开 / 关)
- `org.agent.skillCatalog.group.{inheritedOn,enableable}`(「继承(全局已开)」/「可启用(已安装·全局未开)」)
- `org.agent.skillCatalog.globalEnabled` / `globalDisabled`(行内「全局已启用/未启用」)
- `org.agent.skills.chipTag.inherited`(芯片「继承」小标)
- `org.agent.skills.count`(「继承 {{i}} · 强制开 {{on}} · 强制关 {{off}}」)
- `org.agent.skills.loadingInherited` / 弹窗 footer 覆盖摘要
- 复核既有 `org.agent.skillCatalog.*`、`org.agent.skills.*` 的增删(分组/计数文案变更所致)

## 7. 测试(TDD,Red→Green)

- **`skill_svc.EnabledPluginsMap`**(改语义):强制开→`{id:true}`;强制关→`{id:false}` **在 map 中**;无覆盖的全局已开 → **不在 map 中**(继承)。删旧"全部已安装含 false"断言。
- **`claudeskill.parsePluginList`**:`GloballyEnabled` 由 `enabled` 正确填充(true/false 两例)。
- **`skill_svc.merge` / `ListAgentSkillPacks`**:`GloballyEnabled` 透传到 DTO。
- **前端 `skillPacksToCatalog`**(表测):globally-on→继承组+全局已启用文案;installed-off→可启用组;未装→needInstall。
- **前端 `TriStateToggle`**:三态 active 着色 + `onChange` 回调正确值。
- **前端 `GrantedChips`**:三 tone 样式;继承/强制关无/有 X。
- **前端 `org-detail-agent`**:本地 skills→分段控件 value 映射;开/关/继承三向切换正确 upsert/移除;芯片三态分类;挂载即拉目录(claudecode + CapSkills)。
- **门控真值表不变**:claudecode 显技能区;codex 灰盒;builtin/remote 隐。

## 8. 不做(YAGNI / 范围外)

- skill 级开关(CLI 不支持,只能 plugin 粒度)。
- 远端 / daemon agent 的技能发现(沿用 `2026-06-12` spec §7 延后:`wire.RunParams` 不带 `EnabledPlugins`)。
- 自动安装推荐包(接口预留,不实现)。
- 改任何与本任务无关的文件 / 顺手重构。

## 9. 确认清单(spec review 时请确认)

1. 接受 §2「行为变更」:现有 agent 改为继承全局开的插件(默认 agent ≈ 裸跑 claude 的插件集)。
2. 接受芯片区显继承(§5.3)带来的"打开 agent 详情即 `claude plugin list`"开销。
3. §3 CLI 语义已实测确认(claude 2.1.177),无需回退方案。
