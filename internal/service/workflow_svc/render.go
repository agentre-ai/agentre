// Package workflow_svc — render.go:把用户模板(Go text/template)渲染成注入 Leader 的正文。
package workflow_svc

import (
	"strings"
	"text/template"
)

// DefaultTemplate 默认模板:只放 DAG 占位符。空模板回落到它,
// 保证「带图流程」渲染=DAG 投影,与旧行为逐字节一致。
const DefaultTemplate = "{{ DAGPrompt }}"

// RenderTemplate 用 Go text/template 渲染 tmpl:
//   - 注册函数 DAGPrompt() 返回 dagPrompt(= ProjectGraph 投影);
//   - 数据上下文暴露 {{ .FlowName }};
//   - 用 text/template(非 html/template),提示词不做 HTML 转义;
//   - parse/execute 失败返回 error(调用方据此阻止保存 / 预览显示错误)。
func RenderTemplate(tmpl, name, dagPrompt string) (string, error) {
	t, err := template.New("workflow").Funcs(template.FuncMap{
		"DAGPrompt": func() string { return dagPrompt },
	}).Parse(tmpl)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if err := t.Execute(&b, struct{ FlowName string }{FlowName: name}); err != nil {
		return "", err
	}
	return b.String(), nil
}

// RenderWorkflowContent 是 save 与 preview 的共用渲染:从 graph(若合法)投影出 DAG 提示词 + outline,
// 再用 Go text/template 渲染 template 成 content。保证「预览==注入」。
//   - name 去空白(与保存一致);
//   - 空 template 回落 DefaultTemplate;
//   - graph 不合法(空/无节点)→ dagPrompt 空、outline nil(调用方据此保留原 outline)。
//
// 渲染失败返回 error(调用方阻止保存 / 预览显示错误)。
func RenderWorkflowContent(name, graph, template string) (content string, outline []string, err error) {
	name = strings.TrimSpace(name)
	tmpl := template
	if strings.TrimSpace(tmpl) == "" {
		tmpl = DefaultTemplate
	}
	var dagPrompt string
	if g, ok := ParseFlowGraph(graph); ok {
		dagPrompt, outline = ProjectGraph(name, g)
	}
	content, err = RenderTemplate(tmpl, name, dagPrompt)
	return content, outline, err
}
