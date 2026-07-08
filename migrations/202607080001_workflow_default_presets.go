package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202607080001 用四个内置流程取代旧的单一「Default Orchestration Flow」,并删除 is_default 列。
//   - 每个流程 content == template(手写全文,不含 {{DAGPrompt}});graph 仅供设计器可视化 + 运行时 overlay;
//   - outline 由 ProjectGraph(graph) 投影;content/template/graph/tags/outline 手写,
//     一致性由 202607080001_workflow_default_presets_test.go 锁死(防漂移);
//   - updatetime/createtime 带递减偏移,保证 repo 的 updatetime DESC 排序稳定产出
//     Parallel Decompose 排第一(前端「首个兜底」= items[0])。
func migration202607080001() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202607080001",
		Migrate: func(tx *gorm.DB) error {
			// 1) 删旧内置默认行(趁 is_default 列还在)
			if err := tx.Exec(`DELETE FROM workflows WHERE is_default = 1`).Error; err != nil {
				return err
			}
			// 2) 插 4 个新流程(按 name 守卫幂等)。单基准时间戳(循环前取一次)+ tsOffset
			//    递减偏移,保证 updatetime DESC 排序确定性,不受插入跨秒边界影响。
			var baseTS int64
			if err := tx.Raw(`SELECT CAST(strftime('%s','now') AS INTEGER) * 1000`).Row().Scan(&baseTS); err != nil {
				return err
			}
			for _, f := range presetFlows202607080001 {
				ts := baseTS + int64(f.tsOffset)
				if err := tx.Exec(`INSERT INTO workflows (name, content, template, tags, outline, graph, status, createtime, updatetime)
SELECT ?, ?, ?, ?, ?, ?, 1, ?, ?
WHERE NOT EXISTS (SELECT 1 FROM workflows WHERE name = ?)`,
					f.name, f.prompt, f.prompt, f.tags, f.outline, f.graph, ts, ts, f.name).Error; err != nil {
					return err
				}
			}
			// 3) DROP is_default 列(与 workflow_entity.IsDefault 字段删除同批,见 spec §5)
			return tx.Exec(`ALTER TABLE workflows DROP COLUMN is_default`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			if err := tx.Exec(`ALTER TABLE workflows ADD COLUMN is_default INTEGER NOT NULL DEFAULT 0`).Error; err != nil {
				return err
			}
			for _, f := range presetFlows202607080001 {
				if err := tx.Exec(`DELETE FROM workflows WHERE name = ?`, f.name).Error; err != nil {
					return err
				}
			}
			return nil
		},
	}
}

type presetFlow struct {
	name, prompt, tags, outline, graph string
	tsOffset                           int
}

// presetFlows202607080001 四个内置流程。顺序即 tsOffset 递减 → updatetime DESC 排序。
var presetFlows202607080001 = []presetFlow{
	{
		name:     "Parallel Decompose",
		tsOffset: 3,
		tags:     `["General","Parallel"]`,
		outline:  `["See members","Break down","Subtask A ∥ …","Integrate","Verify","Wrap up"]`,
		graph: `{"version":1,"nodes":[` +
			`{"id":"see","label":"See members","kind":"leader"},` +
			`{"id":"break","label":"Break down","kind":"leader"},` +
			`{"id":"t1","label":"Subtask A","kind":"task","brief":"An independent slice of the work, dispatched to a suitable member. Acceptance: concrete deliverable + criteria met.","sharedFiles":true},` +
			`{"id":"t2","label":"Subtask B","kind":"task","brief":"Another independent slice, dispatched to a suitable member. Acceptance: concrete deliverable + criteria met."},` +
			`{"id":"int","label":"Integrate","kind":"leader"},` +
			`{"id":"ver","label":"Verify","kind":"task","brief":"Run review / tests. Acceptance: all pass, no regressions."},` +
			`{"id":"wrap","label":"Wrap up","kind":"leader"}` +
			`],"edges":[` +
			`{"from":"see","to":"break"},{"from":"break","to":"t1"},{"from":"break","to":"t2"},` +
			`{"from":"t1","to":"int"},{"from":"t2","to":"int"},{"from":"int","to":"ver"},` +
			`{"from":"ver","to":"wrap"},{"from":"ver","to":"t1","kind":"bounce"}` +
			`]}`,
		prompt: `# Parallel Decompose

You are the **Leader** of this run. You do not do the work yourself — you split it, dispatch each piece to the right member, and integrate what comes back. Every result returns to you; you decide the next move.

## Flow
1. **See members.** Call the roster before planning — never assume who is available or what they can do.
2. **Break down.** Split the goal into independent subtasks that can run in parallel without blocking each other. If the work is inherently sequential, use the Pipeline flow instead.
3. **Dispatch in parallel.** Send each subtask to the best-matched member. Every dispatch is one concrete task plus explicit acceptance criteria — vague briefs produce vague work. If two subtasks touch the same files, dispatch with isolate=true so they do not clobber each other.
4. **Integrate.** Pull the results together and resolve conflicts yourself; do not hand integration off to a subtask.
5. **Verify.** Dispatch a review/test pass with a clear bar: all checks green, no regressions. On failure, send the work back to the responsible subtask — do not patch over it yourself.
6. **Wrap up.** Finish with a concise summary @user: what was built, what was verified, and anything still open.
`,
	},
	{
		name:     "Sequential Pipeline",
		tsOffset: 2,
		tags:     `["Sequential","Pipeline"]`,
		outline:  `["Investigate","Design","Implement","Verify","Wrap up"]`,
		graph: `{"version":1,"nodes":[` +
			`{"id":"inv","label":"Investigate","kind":"task","brief":"Gather the facts, constraints, and unknowns. Acceptance: the next stage's questions are answered."},` +
			`{"id":"des","label":"Design","kind":"task","brief":"Produce the approach from the investigation. Acceptance: a concrete design the implementer can follow."},` +
			`{"id":"impl","label":"Implement","kind":"task","brief":"Build against the approved design. Acceptance: design realized, with tests where they apply."},` +
			`{"id":"ver","label":"Verify","kind":"task","brief":"Run review / tests. Acceptance: all pass, no regressions."},` +
			`{"id":"wrap","label":"Wrap up","kind":"leader"}` +
			`],"edges":[` +
			`{"from":"inv","to":"des"},{"from":"des","to":"impl"},{"from":"impl","to":"ver"},` +
			`{"from":"ver","to":"wrap"},{"from":"ver","to":"impl","kind":"bounce"}` +
			`]}`,
		prompt: `# Sequential Pipeline

You are the **Leader** of a staged pipeline. Each stage consumes the previous stage's output, so order matters: never open a stage until its predecessor has met its acceptance criteria. You dispatch each stage, review the handoff, then pass it forward.

## Flow
1. **Investigate.** Dispatch a member to gather the facts, constraints, and unknowns. Acceptance: the questions the next stage needs answered are answered.
2. **Design.** Dispatch the plan/approach based on the investigation. Acceptance: a concrete design the implementer can follow without guessing.
3. **Implement.** Dispatch the build against the approved design. Acceptance: the design is realized, with tests where they apply.
4. **Verify.** Dispatch review/tests. Acceptance: all pass, no regressions. On failure, send it back to Implement — fix the stage that broke, do not bolt on a new one.
5. **Wrap up.** Finish with a concise summary @user: what each stage produced and what was verified.

Between every stage, confirm the handoff is solid before moving on. A weak earlier stage poisons everything downstream.
`,
	},
	{
		name:     "Research → Synthesize",
		tsOffset: 1,
		tags:     `["Research","Knowledge"]`,
		outline:  `["Frame questions","Angle A ∥ …","Synthesize","Wrap up"]`,
		graph: `{"version":1,"nodes":[` +
			`{"id":"frame","label":"Frame questions","kind":"leader"},` +
			`{"id":"a1","label":"Angle A","kind":"task","brief":"Investigate one angle. Return findings with sources/evidence, not opinions."},` +
			`{"id":"a2","label":"Angle B","kind":"task","brief":"Investigate a second angle. Return findings with sources/evidence."},` +
			`{"id":"a3","label":"Angle C","kind":"task","brief":"Investigate a third angle. Return findings with sources/evidence."},` +
			`{"id":"syn","label":"Synthesize","kind":"leader"},` +
			`{"id":"wrap","label":"Wrap up","kind":"leader"}` +
			`],"edges":[` +
			`{"from":"frame","to":"a1"},{"from":"frame","to":"a2"},{"from":"frame","to":"a3"},` +
			`{"from":"a1","to":"syn"},{"from":"a2","to":"syn"},{"from":"a3","to":"syn"},` +
			`{"from":"syn","to":"wrap"}` +
			`]}`,
		prompt: `# Research → Synthesize

You are the **Leader** of a research effort. This flow produces understanding, not code: you frame the questions, fan out independent investigations, then converge the findings into one coherent answer. Every finding returns to you.

## Flow
1. **Frame the questions.** State what you actually need to know and the distinct angles worth investigating. Good framing keeps the parallel work non-overlapping.
2. **Investigate in parallel.** Dispatch one member per angle. Each returns findings with sources/evidence, not opinions — an unsourced claim is a lead, not a conclusion. Angles are independent; they do not wait on each other.
3. **Synthesize.** Pull every angle together yourself. Reconcile conflicts, note where sources disagree, and separate what is well-supported from what is uncertain. Do not just concatenate the reports.
4. **Wrap up.** Deliver a concise report @user: the answer, the confidence behind it, the key evidence, and what remains open.

Do not write or verify code in this flow — if the task turns into building something, switch to Parallel Decompose or Pipeline.
`,
	},
	{
		name:     "Generate → Review → Iterate",
		tsOffset: 0,
		tags:     `["Quality","Loop"]`,
		outline:  `["Produce","Review","Wrap up"]`,
		graph: `{"version":1,"nodes":[` +
			`{"id":"prod","label":"Produce","kind":"task","brief":"Produce the work against concrete, testable acceptance criteria."},` +
			`{"id":"rev","label":"Review","kind":"task","brief":"A separate member reviews adversarially. Acceptance: criteria genuinely met."},` +
			`{"id":"wrap","label":"Wrap up","kind":"leader"}` +
			`],"edges":[` +
			`{"from":"prod","to":"rev"},{"from":"rev","to":"wrap"},{"from":"rev","to":"prod","kind":"bounce"}` +
			`]}`,
		prompt: `# Generate → Review → Iterate

You are the **Leader** of a quality-gated loop. One member produces, a *different* member reviews adversarially, and nothing ships until it passes. The whole point is the separation: the reviewer must not be the producer.

## Flow
1. **Produce.** Dispatch the work with a concrete task and explicit acceptance criteria. Acceptance is what Review will hold it to, so make it testable.
2. **Review.** Dispatch a *separate* member to review adversarially — actively try to break it, find the gaps, check the claims. A rubber-stamp review is worse than none. Acceptance: the reviewer signs off that the criteria are genuinely met.
3. **Iterate.** On any real issue, send it back to Produce with the specific defects — reuse the same node, do not spawn a new one. Loop until Review passes clean.
4. **Wrap up.** Finish with a concise summary @user: what was produced, what Review caught, and how it was resolved.

Keep producer and reviewer distinct across iterations so the review stays honest.
`,
	},
}
