/* eslint-disable i18next/no-literal-string */
import * as React from "react";
import {
  Bot,
  Cable,
  Check,
  ChevronDown,
  CircleHelp,
  Cpu,
  Database,
  FileText,
  Gauge,
  Keyboard,
  LayoutGrid,
  MessageSquare,
  MoreHorizontal,
  Network,
  Pencil,
  Play,
  Plus,
  Radar,
  RotateCw,
  SendHorizontal,
  Server,
  Settings,
  ShieldAlert,
  ShieldCheck,
  Sparkles,
  SunMoon,
  TerminalSquare,
  Trash2,
  Wrench,
  X,
} from "lucide-react";

import { AgentreDialog } from "@/components/agentre/app-dialog";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

const navSections = [
  {
    label: "通用",
    items: [
      [SunMoon, "外观"],
      [MessageSquare, "通知"],
      [Keyboard, "快捷键"],
      [Database, "数据备份"],
    ],
  },
  {
    label: "引擎",
    items: [
      [Sparkles, "LLM 提供商"],
      [Cpu, "Agent 后端"],
    ],
  },
  {
    label: "集成",
    items: [
      [Network, "本地代理"],
      [Server, "MCP 服务器"],
      [Wrench, "Skills 与工具"],
      [Cable, "远程设备"],
    ],
  },
  { label: "关于", items: [[CircleHelp, "版本与日志"]] },
] as const;

function Label({ children }: { children: React.ReactNode }) {
  return <label className="text-xs font-medium leading-none">{children}</label>;
}

const backends = [
  {
    name: "Built-in Agent",
    type: "内置",
    detail: "2 个 Agent 正在使用",
    endpoint: "—",
    provider: "OpenAI · gpt-5.6",
    icon: LayoutGrid,
    ok: true,
  },
  {
    name: "Claude Code",
    type: "Claude Code",
    detail: "1 个 Agent 正在使用",
    endpoint: "/usr/local/bin/claude",
    provider: "CLI 登录",
    icon: TerminalSquare,
    ok: true,
  },
  {
    name: "OpenClaw Gateway",
    type: "OpenClaw",
    detail: "3 个 Agent 正在使用",
    endpoint: "ws://127.0.0.1:18789",
    provider: "main · huu/gpt-5.6-sol",
    icon: Bot,
    ok: true,
    selected: true,
  },
  {
    name: "Codex",
    type: "Codex",
    detail: "未使用",
    endpoint: "/usr/local/bin/codex",
    provider: "OpenAI Responses · codex",
    icon: FileText,
    ok: true,
  },
];

function AppRail({ active }: { active: "settings" | "chat" }) {
  const items = [
    [MessageSquare, "聊天", "chat"],
    [LayoutGrid, "项目", "projects"],
    [FileText, "问题", "issues"],
    [Gauge, "Hooks", "hooks"],
  ] as const;
  return (
    <aside className="flex w-14 shrink-0 flex-col items-center gap-1 border-r border-border bg-rail px-2 py-3">
      {items.map(([Icon, label, id]) => (
        <Button
          key={id}
          variant="ghost"
          size="icon"
          title={label}
          className={`size-9 ${active === id ? "bg-card text-primary-text shadow-sm" : "text-muted-foreground"}`}
        >
          <Icon className="size-4" />
        </Button>
      ))}
      <Button
        variant="ghost"
        size="icon"
        className="mt-auto size-9 text-muted-foreground"
      >
        <SunMoon className="size-4" />
      </Button>
      <Button
        variant="ghost"
        size="icon"
        title="设置"
        className={`size-9 ${active === "settings" ? "bg-card text-primary-text shadow-sm" : "text-muted-foreground"}`}
      >
        <Settings className="size-4" />
      </Button>
    </aside>
  );
}

function StatusBar() {
  return (
    <footer className="flex h-7 shrink-0 items-center justify-between border-t border-border bg-card px-3 font-mono text-[10px] text-muted-foreground">
      <span className="inline-flex items-center gap-1.5">
        <span className="size-1.5 rounded-full bg-status-running" />
        Gateway 已连接
      </span>
      <span>AgentRE · OpenClaw Mockup</span>
    </footer>
  );
}

function SettingsSidebar() {
  return (
    <aside className="flex w-[220px] shrink-0 flex-col gap-[18px] border-r border-border bg-sidebar px-3 py-4">
      <div className="px-1.5 pb-2 text-sm font-semibold">设置</div>
      {navSections.map((section) => (
        <div key={section.label} className="flex flex-col gap-1">
          <div className="px-2.5 pb-1 font-mono text-[10px] font-semibold uppercase tracking-[0.08em] text-subtle-foreground">
            {section.label}
          </div>
          {section.items.map(([Icon, label]) => {
            const selected = label === "Agent 后端";
            return (
              <Button
                key={label}
                variant="ghost"
                className={`h-[30px] w-full justify-start gap-2 px-2.5 text-sm font-normal ${selected ? "bg-primary-soft font-medium text-primary-text hover:bg-primary-soft hover:text-primary-text" : "text-foreground"}`}
              >
                <Icon className="size-4" />
                {label}
              </Button>
            );
          })}
        </div>
      ))}
    </aside>
  );
}

function BackendTable({ onOpen }: { onOpen: () => void }) {
  return (
    <section className="min-w-0 overflow-hidden rounded-lg border border-border bg-card">
      <div className="flex items-center justify-between border-b border-border px-4 py-3">
        <div>
          <div className="text-sm font-semibold">Agent 后端</div>
          <div className="text-[11px] text-muted-foreground">共 4 个后端</div>
        </div>
        <div className="flex gap-2">
          <Button
            variant="outline"
            size="sm"
            className="h-[30px] gap-1.5 px-3 text-xs"
          >
            <Radar className="size-3.5" />
            自动扫描
          </Button>
          <Button
            size="sm"
            className="h-[30px] gap-1.5 px-3 text-xs"
            onClick={onOpen}
          >
            <Plus className="size-3.5" />
            添加后端
          </Button>
        </div>
      </div>
      <div className="overflow-x-auto">
        <Table className="min-w-[980px]">
          <TableHeader>
            <TableRow className="bg-secondary hover:bg-secondary">
              <TableHead className="w-[260px] px-4 font-mono text-[10px] font-semibold uppercase tracking-[0.08em] text-muted-foreground">
                名称
              </TableHead>
              <TableHead className="w-[180px] font-mono text-[10px] font-semibold uppercase tracking-[0.08em] text-muted-foreground">
                类型
              </TableHead>
              <TableHead className="min-w-[260px] font-mono text-[10px] font-semibold uppercase tracking-[0.08em] text-muted-foreground">
                运行入口
              </TableHead>
              <TableHead className="w-[250px] font-mono text-[10px] font-semibold uppercase tracking-[0.08em] text-muted-foreground">
                Agent / 模型
              </TableHead>
              <TableHead className="w-[120px]" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {backends.map((backend) => {
              const Icon = backend.icon;
              return (
                <TableRow
                  key={backend.name}
                  className={
                    backend.selected
                      ? "bg-primary-soft/45 hover:bg-primary-soft/60"
                      : "hover:bg-accent/45"
                  }
                >
                  <TableCell className="px-4 py-3">
                    <div className="flex items-center gap-3">
                      <span className="size-1.5 rounded-full bg-status-running" />
                      <div className="min-w-0">
                        <div className="flex items-center gap-1.5 text-sm font-medium">
                          {backend.name}
                          {backend.selected ? (
                            <Badge
                              variant="secondary"
                              className="rounded-sm px-1.5 py-0 font-mono text-[10px] text-primary-text"
                            >
                              Gateway RPC
                            </Badge>
                          ) : null}
                        </div>
                        <div className="font-mono text-[10px] text-subtle-foreground">
                          {backend.detail}
                        </div>
                      </div>
                    </div>
                  </TableCell>
                  <TableCell className="py-3 text-xs">
                    <span className="inline-flex items-center gap-1.5">
                      <Icon className="size-3.5 text-primary-text" />
                      {backend.type}
                    </span>
                  </TableCell>
                  <TableCell className="py-3 font-mono text-[11px] text-muted-foreground">
                    {backend.endpoint}
                  </TableCell>
                  <TableCell className="py-3 font-mono text-[10px]">
                    <span className="inline-flex items-center gap-1.5">
                      <Sparkles className="size-3 text-muted-foreground" />
                      {backend.provider}
                    </span>
                  </TableCell>
                  <TableCell className="px-4 py-3">
                    <div className="flex justify-end gap-1">
                      <Button
                        variant="ghost"
                        size="icon"
                        className="size-[26px] text-muted-foreground"
                      >
                        <SendHorizontal className="size-3.5" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="size-[26px] text-muted-foreground"
                        onClick={backend.selected ? onOpen : undefined}
                      >
                        <Pencil className="size-3.5" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="size-[26px] text-muted-foreground"
                      >
                        <Trash2 className="size-3.5" />
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      </div>
    </section>
  );
}

function OpenClawDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [advanced, setAdvanced] = React.useState(true);
  return (
    <AgentreDialog
      open={open}
      onOpenChange={onOpenChange}
      title="配置 OpenClaw Gateway"
      description="通过 Gateway WebSocket RPC 连接 OpenClaw，支持完整 session、事件、工具和中断能力。"
      contentClassName="max-w-[680px]"
      bodyClassName="max-h-none overflow-visible py-3"
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button>
            <Check className="size-4" />
            保存后端
          </Button>
        </>
      }
    >
      <div className="grid gap-3">
        <div className="grid grid-cols-2 gap-4">
          <div className="grid gap-1.5">
            <Label>后端名称</Label>
            <Input defaultValue="OpenClaw Gateway" />
          </div>
          <div className="grid gap-1.5">
            <Label>运行设备</Label>
            <Select defaultValue="local">
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="local">本机</SelectItem>
                <SelectItem value="remote">远程 agentred</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>
        <div className="grid gap-1.5">
          <Label>Gateway WebSocket 地址</Label>
          <div className="flex gap-2">
            <Input className="font-mono" defaultValue="ws://127.0.0.1:18789" />
            <Button variant="outline" className="shrink-0 gap-1.5">
              <Play className="size-3.5" />
              测试连接
            </Button>
          </div>
          <p className="text-[11px] text-muted-foreground">
            本机允许 ws://；非 loopback 地址必须使用 wss://。
          </p>
        </div>
        <div className="grid gap-1.5">
          <Label>Gateway Token</Label>
          <Input type="password" defaultValue="openclaw-secret-placeholder" />
          <p className="text-[11px] text-muted-foreground">
            Token 保存到专用 secret storage，编辑时不会回显原值。
          </p>
        </div>
        <Alert className="border-status-running/30 bg-status-running-bg">
          <ShieldCheck className="size-4 text-status-running" />
          <AlertTitle className="text-xs">协议握手成功 · 42ms</AlertTitle>
          <AlertDescription className="text-[11px]">
            Protocol 4 · operator.read / operator.write / operator.approvals ·
            Gateway 2026.7.1-2
          </AlertDescription>
        </Alert>
        <div className="grid grid-cols-2 gap-4">
          <div className="grid gap-1.5">
            <Label>OpenClaw Agent</Label>
            <Select defaultValue="main">
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="main">main</SelectItem>
                <SelectItem value="design">design</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className="grid gap-1.5">
            <Label>默认模型</Label>
            <Select defaultValue="inherit">
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="inherit">继承 Agent 配置</SelectItem>
                <SelectItem value="huu">huu/gpt-5.6-sol</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>
        <div className="grid gap-1.5">
          <Label>Session 映射策略</Label>
          <Select defaultValue="per-chat">
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="per-chat">
                每条 AgentRE 会话映射一条 OpenClaw session
              </SelectItem>
              <SelectItem value="shared">所有会话共享固定 session</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <button
          type="button"
          className="flex items-center justify-between border-t border-border pt-4 text-left text-xs font-medium"
          onClick={() => setAdvanced(!advanced)}
        >
          <span>连接与恢复策略</span>
          <ChevronDown
            className={`size-4 transition-transform ${advanced ? "rotate-180" : ""}`}
          />
        </button>
        {advanced ? (
          <div className="grid grid-cols-2 gap-4 rounded-md bg-secondary p-3">
            <div className="grid gap-1.5">
              <Label>连接超时</Label>
              <Input defaultValue="10s" />
            </div>
            <div className="grid gap-1.5">
              <Label>Turn 超时</Label>
              <Input defaultValue="0（不限制）" />
            </div>
            <div className="grid gap-1.5">
              <Label>重连策略</Label>
              <Select defaultValue="reconcile">
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="reconcile">指数退避并对账</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="grid gap-1.5">
              <Label>删除本地会话</Label>
              <Select defaultValue="keep">
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="keep">保留 OpenClaw session</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>
        ) : null}
      </div>
    </AgentreDialog>
  );
}

function SettingsView({ initialDialog = false }: { initialDialog?: boolean }) {
  const [open, setOpen] = React.useState(initialDialog);
  return (
    <div className="flex h-screen flex-col bg-background text-foreground">
      <div className="flex min-h-0 flex-1">
        <AppRail active="settings" />
        <SettingsSidebar />
        <main className="min-w-0 flex-1 overflow-auto bg-background">
          <div className="flex min-h-full w-full max-w-[1180px] flex-col gap-8 px-10 py-8">
            <header className="flex flex-col gap-1">
              <h1 className="text-xl font-semibold tracking-tight">
                Agent 后端
              </h1>
              <p className="max-w-3xl text-sm text-muted-foreground">
                管理 AgentRE 用于运行会话的本地、CLI 与 Gateway-native 后端。
              </p>
            </header>
            <BackendTable onOpen={() => setOpen(true)} />
            <Alert>
              <Cpu className="size-4" />
              <AlertTitle className="text-xs">运行位置</AlertTitle>
              <AlertDescription className="text-[11px]">
                本机后端由桌面进程运行；绑定远程设备后，由对应 agentred 连接
                OpenClaw Gateway。
              </AlertDescription>
            </Alert>
          </div>
        </main>
      </div>
      <StatusBar />
      <OpenClawDialog open={open} onOpenChange={setOpen} />
    </div>
  );
}

function ChatView() {
  return (
    <div className="flex h-screen flex-col bg-background text-foreground">
      <div className="flex min-h-0 flex-1">
        <AppRail active="chat" />
        <aside className="flex w-[252px] shrink-0 flex-col border-r border-border bg-sidebar">
          <div className="flex h-12 items-center justify-between border-b border-border px-3">
            <span className="text-sm font-semibold">聊天</span>
            <Button size="icon" variant="ghost" className="size-7">
              <Plus className="size-4" />
            </Button>
          </div>
          <div className="p-2">
            <div className="rounded-md bg-card px-3 py-2 shadow-sm">
              <div className="truncate text-xs font-medium">
                验证 OpenClaw Gateway 集成
              </div>
              <div className="mt-1 font-mono text-[10px] text-muted-foreground">
                OpenClaw · main
              </div>
            </div>
          </div>
        </aside>
        <main className="flex min-w-0 flex-1 flex-col">
          <div className="flex h-10 items-center justify-between border-b border-border bg-card px-3">
            <div className="flex items-center gap-2 text-xs font-medium">
              <Bot className="size-3.5 text-primary-text" />
              OpenClaw Gateway
              <Badge
                variant="secondary"
                className="rounded-sm px-1.5 py-0 font-mono text-[10px] text-status-running"
              >
                已连接
              </Badge>
            </div>
            <div className="flex items-center gap-1">
              <Button variant="ghost" size="icon" className="size-7">
                <RotateCw className="size-3.5" />
              </Button>
              <Button variant="ghost" size="icon" className="size-7">
                <MoreHorizontal className="size-3.5" />
              </Button>
            </div>
          </div>
          <div className="flex-1 overflow-auto px-10 py-7">
            <div className="mx-auto flex max-w-[820px] flex-col gap-5">
              <div className="flex justify-end">
                <div className="max-w-[72%] rounded-lg bg-primary px-3.5 py-2.5 text-sm text-primary-foreground">
                  检查这个仓库的测试状态，并告诉我失败原因。
                </div>
              </div>
              <div className="flex gap-3">
                <div className="mt-0.5 flex size-7 shrink-0 items-center justify-center rounded-md bg-primary-soft text-primary-text">
                  <Bot className="size-4" />
                </div>
                <div className="min-w-0 flex-1 space-y-3">
                  <div className="text-sm leading-6">
                    我先读取项目结构和测试脚本，然后运行最小测试集。
                  </div>
                  <div className="overflow-hidden rounded-md border border-border bg-card">
                    <div className="flex items-center justify-between border-b border-border px-3 py-2">
                      <div className="flex items-center gap-2 text-xs font-medium">
                        <TerminalSquare className="size-3.5 text-primary-text" />
                        exec
                      </div>
                      <Badge
                        variant="secondary"
                        className="rounded-sm px-1.5 py-0 font-mono text-[10px] text-status-running"
                      >
                        已完成
                      </Badge>
                    </div>
                    <pre className="bg-code-surface px-3 py-2.5 font-mono text-[11px] leading-5 text-code-foreground">
                      pnpm test -- --runInBand{"\n"}✓ 128 passed{"\n"}✗ 1 failed
                      · gateway reconnect
                    </pre>
                  </div>
                  <div className="overflow-hidden rounded-md border border-status-waiting/45 bg-status-waiting-bg">
                    <div className="flex items-start justify-between gap-3 border-b border-status-waiting/25 px-3 py-2.5">
                      <div className="flex gap-2">
                        <ShieldAlert className="mt-0.5 size-4 shrink-0 text-status-waiting" />
                        <div>
                          <div className="text-xs font-semibold">
                            OpenClaw 请求执行权限
                          </div>
                          <div className="mt-0.5 text-[10px] text-muted-foreground">
                            exec approval · Gateway host
                          </div>
                        </div>
                      </div>
                      <Badge
                        variant="secondary"
                        className="shrink-0 rounded-sm px-1.5 py-0 font-mono text-[10px] text-status-waiting"
                      >
                        等待审批
                      </Badge>
                    </div>
                    <div className="space-y-2.5 px-3 py-2.5 text-[11px]">
                      <code className="block rounded border border-border bg-code-surface px-2.5 py-2 font-mono text-code-foreground">
                        go test ./internal/pkg/agentruntime/... -race
                      </code>
                      <div className="grid grid-cols-3 gap-x-3 gap-y-1 font-mono text-[11px] text-muted-foreground">
                        <span>Agent: main</span>
                        <span>Host: gateway</span>
                        <span>剩余: 29:42</span>
                        <span className="col-span-2 truncate">
                          CWD: /root/code/agentre/agentre
                        </span>
                        <span>ask: on-miss</span>
                      </div>
                      <div className="flex items-start gap-1.5 rounded bg-status-waiting/10 px-2 py-1.5 text-[10px] text-muted-foreground">
                        <ShieldCheck className="mt-px size-3 shrink-0 text-status-waiting" />
                        “始终允许”会把本命令规则写入该执行宿主上 main Agent 的
                        allowlist。
                      </div>
                      <div className="flex flex-wrap gap-2">
                        <Button size="sm" className="h-7 text-xs">
                          <Play className="size-3" />
                          允许一次
                        </Button>
                        <Button
                          size="sm"
                          variant="outline"
                          className="h-7 text-xs"
                        >
                          <ShieldCheck className="size-3" />
                          始终允许
                        </Button>
                        <Button
                          size="sm"
                          variant="outline"
                          className="h-7 text-xs text-destructive hover:text-destructive"
                        >
                          <X className="size-3" />
                          拒绝
                        </Button>
                      </div>
                      <div className="font-mono text-[10px] text-muted-foreground">
                        approval: 7b61c9… · session: agentre:3:184
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </div>
          <div className="border-t border-border bg-card px-8 py-3">
            <div className="mx-auto flex max-w-[820px] items-end gap-2 rounded-lg border border-input bg-background p-2 shadow-sm">
              <div className="min-h-10 flex-1 px-2 py-2 text-sm text-muted-foreground">
                输入消息，或使用 / 调用命令…
              </div>
              <Button size="icon" className="size-8">
                <SendHorizontal className="size-4" />
              </Button>
            </div>
            <div className="mx-auto mt-1.5 flex max-w-[820px] justify-between font-mono text-[10px] text-muted-foreground">
              <span>session: agentre:3:184 · protocol 4</span>
              <span>Esc 中断</span>
            </div>
          </div>
        </main>
      </div>
      <StatusBar />
    </div>
  );
}

export function OpenClawIntegrationMockup() {
  const params = new URLSearchParams(window.location.search);
  const view = params.get("view") ?? "list";
  if (view === "chat") return <ChatView />;
  return <SettingsView initialDialog={view === "dialog"} />;
}
