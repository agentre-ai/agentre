import * as React from "react";
import {
  AlertTriangle,
  Folder,
  FolderTree,
  Loader2,
  Pencil,
  Plus,
  Trash2,
  Users,
  X,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { useChatAgents } from "@/hooks/use-chat-agents";
import { cn } from "@/lib/utils";

import {
  ProjectAddMember,
  ProjectDelete,
  ProjectGet,
  ProjectLocationList,
  ProjectLocationRemove,
  ProjectLocationUpsert,
  ProjectRemoveMember,
  ProjectUpdate,
  RemoteDeviceList,
} from "../../../wailsjs/go/app/App";
import type { chat_svc, app } from "../../../wailsjs/go/models";
import { DeviceTag } from "./device-tag";
import { AgentAvatar } from "./primitives";
import { RemoteFsPicker } from "./remote-fs-picker";
import {
  agentColorClassNames,
  agentColorOrder,
  type AgentColor,
} from "./types";

type ProjectDetailResponse = app.ProjectDetailResponse;
type ProjectMemberItem = app.ProjectMemberItem & {
  agentName?: string;
  avatarColor?: string;
  avatarIcon?: string;
  avatarDataUrl?: string;
};
// wailsjs codegen refreshes on `make dev`; this intersection preserves TS safety
// for remote-device fields while generated bindings are stale.
type ChatAgentItem = chat_svc.ChatAgentItem & {
  deviceID?: string;
  deviceName?: string;
  online?: boolean;
};

// ProjectLocationView mirrors project_location_svc.ProjectLocationView.
// wailsjs codegen will replace this when `make dev` / `wails build` runs.
type ProjectLocationView = {
  id: number;
  projectId: number;
  deviceId: string;
  path: string;
  deviceName: string;
  online: boolean;
};

// DeviceView mirrors remote_device_svc.DeviceView; full structure replaced by codegen.
type DeviceView = {
  id: number;
  name: string;
  online: boolean;
};

export type ProjectSettingsDrawerProps = {
  /** 0 = 关闭；>0 = 打开并加载该项目 */
  projectID: number;
  onClose: () => void;
  onChanged: () => void; // 任意编辑后让 page 刷新树
  onDeleted: () => void;
};

type TabKey = "basic" | "members" | "locations" | "danger";

const tabs: { key: TabKey; label: string }[] = [
  { key: "basic", label: "基本" },
  { key: "members", label: "成员" },
  { key: "locations", label: "远端路径" },
  { key: "danger", label: "危险区" },
];

function ProjectSettingsDrawer({
  projectID,
  onClose,
  onChanged,
  onDeleted,
}: ProjectSettingsDrawerProps) {
  const open = projectID > 0;
  const [detail, setDetail] = React.useState<ProjectDetailResponse | null>(
    null,
  );
  const [loading, setLoading] = React.useState(false);
  const [activeTab, setActiveTab] = React.useState<TabKey>("basic");

  const reload = React.useCallback(async () => {
    if (projectID <= 0) return;
    setLoading(true);
    try {
      const d = await ProjectGet(projectID);
      setDetail(d);
    } catch {
      setDetail(null);
    } finally {
      setLoading(false);
    }
  }, [projectID]);

  React.useEffect(() => {
    if (open) {
      setActiveTab("basic");
      void reload();
    } else {
      setDetail(null);
    }
  }, [open, reload]);

  return (
    <Dialog open={open} onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="max-w-[460px]" showCloseButton>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            {detail?.project?.name ?? "项目设置"}
            <span className="font-mono text-2xs font-normal text-muted-foreground">
              · 项目设置
            </span>
          </DialogTitle>
        </DialogHeader>

        {/* Tabs */}
        <div className="flex items-center gap-1 border-b border-border px-4">
          {tabs.map((t) => (
            <button
              key={t.key}
              type="button"
              onClick={() => setActiveTab(t.key)}
              className={cn(
                "relative px-2 py-2 text-xs transition-colors",
                activeTab === t.key
                  ? "font-semibold text-foreground"
                  : "text-muted-foreground hover:text-foreground",
              )}
            >
              {t.label}
              {activeTab === t.key ? (
                <span
                  aria-hidden="true"
                  className="absolute inset-x-0 -bottom-px h-[2px] bg-primary"
                />
              ) : null}
            </button>
          ))}
        </div>

        <DialogBody className="flex max-h-[60vh] flex-col gap-3">
          {loading || !detail ? (
            <div className="flex items-center justify-center py-10 text-xs text-muted-foreground">
              <Loader2
                className="mr-2 size-3.5 animate-spin"
                aria-hidden="true"
              />
              加载中…
            </div>
          ) : activeTab === "basic" ? (
            <BasicTab
              detail={detail}
              onSaved={() => {
                void reload();
                onChanged();
              }}
            />
          ) : activeTab === "members" ? (
            <MembersTab
              detail={detail}
              onChanged={() => {
                void reload();
                onChanged();
              }}
            />
          ) : activeTab === "locations" ? (
            <LocationsTab detail={detail} />
          ) : (
            <DangerTab
              detail={detail}
              onDeleted={() => {
                onClose();
                onDeleted();
              }}
            />
          )}
        </DialogBody>

        <DialogFooter>
          <Button type="button" variant="ghost" onClick={onClose}>
            关闭
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ─── Tabs ─────────────────────────────────────────────────────────────────────

function BasicTab({
  detail,
  onSaved,
}: {
  detail: ProjectDetailResponse;
  onSaved: () => void;
}) {
  const p = detail.project!;
  const [name, setName] = React.useState(p.name);
  const [icon, setIcon] = React.useState(p.icon);
  const [color, setColor] = React.useState<AgentColor>(
    (p.color as AgentColor) || "agent-1",
  );
  const [description, setDescription] = React.useState(p.description);
  const [saving, setSaving] = React.useState(false);
  const [err, setErr] = React.useState<string | null>(null);

  const dirty =
    name.trim() !== p.name ||
    icon !== p.icon ||
    color !== p.color ||
    description !== p.description;

  const handleSave = async () => {
    setErr(null);
    setSaving(true);
    try {
      await ProjectUpdate({
        id: p.id,
        name: name.trim(),
        icon,
        color,
        description: description.trim(),
      });
      onSaved();
    } catch (e) {
      setErr(String(e));
    } finally {
      setSaving(false);
    }
  };

  return (
    <>
      <Field label="项目名">
        <Input
          value={name}
          onChange={(e) => setName(e.target.value)}
          className="h-9 text-xs"
        />
      </Field>
      <Field label="图标 key">
        <Input
          value={icon}
          onChange={(e) => setIcon(e.target.value)}
          className="h-9 font-mono text-xs"
          placeholder="folder / briefcase / 自定义 emoji"
        />
      </Field>
      <Field label="主题色">
        <div className="flex items-center gap-1.5">
          {agentColorOrder.slice(0, 5).map((c) => (
            <button
              key={c}
              type="button"
              aria-label={c}
              onClick={() => setColor(c)}
              className={cn(
                "size-6 rounded-full",
                agentColorClassNames[c],
                color === c &&
                  "outline outline-2 outline-offset-2 outline-foreground",
              )}
            />
          ))}
        </div>
      </Field>
      <Field label="描述">
        <Textarea
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          className="min-h-[60px] text-xs"
        />
      </Field>
      <Field label="本地路径（只读）">
        <Input
          value={p.path}
          readOnly
          className="h-9 cursor-default bg-secondary/50 font-mono text-xs text-muted-foreground"
        />
      </Field>
      {err ? (
        <div className="rounded-md border border-destructive bg-destructive-soft px-3 py-2 text-2xs text-destructive">
          {err}
        </div>
      ) : null}
      <div className="mt-1 flex justify-end">
        <Button
          type="button"
          disabled={!dirty || saving}
          onClick={() => void handleSave()}
        >
          {saving ? (
            <Loader2 className="size-3.5 animate-spin" aria-hidden="true" />
          ) : null}
          保存
        </Button>
      </div>
    </>
  );
}

function MembersTab({
  detail,
  onChanged,
}: {
  detail: ProjectDetailResponse;
  onChanged: () => void;
}) {
  const p = detail.project!;
  const { agents } = useChatAgents();
  const [picking, setPicking] = React.useState(false);
  const [busyAgent, setBusyAgent] = React.useState<number | null>(null);
  const [err, setErr] = React.useState<string | null>(null);
  const [locations, setLocations] = React.useState<
    Record<string, ProjectLocationView>
  >({});

  React.useEffect(() => {
    void ProjectLocationList(p.id).then((rows) => {
      const map: Record<string, ProjectLocationView> = {};
      for (const r of rows ?? []) {
        if (r?.deviceId) map[r.deviceId] = r as ProjectLocationView;
      }
      setLocations(map);
    });
  }, [p.id, detail.directMembers]);

  const directIDs = new Set((detail.directMembers ?? []).map((m) => m.agentID));
  const inheritedIDs = new Set(
    (detail.inheritedMembers ?? []).map((m) => m.agentID),
  );
  const candidates = agents.filter(
    (a) => !directIDs.has(a.id) && !inheritedIDs.has(a.id),
  );

  const removeMember = async (agentID: number) => {
    setErr(null);
    setBusyAgent(agentID);
    try {
      await ProjectRemoveMember(p.id, agentID);
      onChanged();
    } catch (e) {
      setErr(String(e));
    } finally {
      setBusyAgent(null);
    }
  };

  const agentByID = new Map(agents.map((a) => [a.id, a]));

  return (
    <>
      <SectionLabel
        icon={<Users className="size-3.5" aria-hidden="true" />}
        label={`直接成员 · ${detail.directMembers?.length ?? 0}`}
      />
      {(detail.directMembers ?? []).length === 0 ? (
        <EmptyHint text="还没有直接成员；可以从下面候选里加。" />
      ) : (
        <ul className="flex flex-col gap-1">
          {(detail.directMembers ?? []).map((m) => {
            const agent = agentByID.get(m.agentID);
            const location = agent?.deviceID
              ? locations[agent.deviceID]
              : undefined;
            return (
              <MemberRow
                key={`d-${m.agentID}`}
                member={m}
                agent={agent}
                location={location}
                onRemove={() => void removeMember(m.agentID)}
                busy={busyAgent === m.agentID}
                inherited={false}
              />
            );
          })}
        </ul>
      )}

      {detail.inheritedMembers && detail.inheritedMembers.length > 0 ? (
        <>
          <SectionLabel
            label={`继承自父项目 · ${detail.inheritedMembers.length}`}
          />
          <ul className="flex flex-col gap-1">
            {detail.inheritedMembers.map((m) => {
              const agent = agentByID.get(m.agentID);
              const location = agent?.deviceID
                ? locations[agent.deviceID]
                : undefined;
              return (
                <MemberRow
                  key={`i-${m.agentID}`}
                  member={m}
                  agent={agent}
                  location={location}
                  onRemove={() => {}}
                  busy={false}
                  inherited
                />
              );
            })}
          </ul>
        </>
      ) : null}

      <div className="border-t border-border pt-2">
        {picking ? (
          <div className="flex flex-col gap-1.5">
            <SectionLabel label="选择 Agent" />
            {candidates.length === 0 ? (
              <EmptyHint text="所有 Agent 都已经在本项目（或父项目继承）。" />
            ) : (
              <ul className="flex max-h-40 flex-col gap-1 overflow-auto">
                {candidates.map((a) => (
                  <li key={a.id}>
                    <CandidateRow
                      agent={a}
                      existingLocation={
                        a.deviceID ? locations[a.deviceID] : undefined
                      }
                      busy={busyAgent === a.id}
                      onAdd={async () => {
                        setErr(null);
                        setBusyAgent(a.id);
                        try {
                          await ProjectAddMember(p.id, a.id);
                          setPicking(false);
                          onChanged();
                        } catch (e) {
                          setErr(String(e));
                        } finally {
                          setBusyAgent(null);
                        }
                      }}
                    />
                  </li>
                ))}
              </ul>
            )}
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="h-7 text-2xs"
              onClick={() => setPicking(false)}
            >
              取消
            </Button>
          </div>
        ) : (
          <div className="flex flex-col gap-1">
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="h-7 gap-1 self-start text-2xs"
              onClick={() => setPicking(true)}
            >
              <Plus className="size-3.5" aria-hidden="true" />
              添加 Agent
            </Button>
            <span className="text-2xs text-muted-foreground">
              远端 Agent 需要先在「远端路径」tab 配置该设备的路径
            </span>
          </div>
        )}
      </div>
      {err ? (
        <div className="rounded-md border border-destructive bg-destructive-soft px-3 py-2 text-2xs text-destructive">
          {err}
        </div>
      ) : null}
    </>
  );
}

function MemberRow({
  member,
  agent,
  location,
  onRemove,
  busy,
  inherited,
}: {
  member: ProjectMemberItem;
  agent?: ChatAgentItem;
  location?: ProjectLocationView | null;
  onRemove: () => void;
  busy: boolean;
  inherited: boolean;
}) {
  const color =
    (member.avatarColor as AgentColor) ||
    (agent?.avatarColor as AgentColor) ||
    "agent-1";
  const name = member.agentName || agent?.name || `Agent #${member.agentID}`;
  const avatarIcon = member.avatarIcon || agent?.avatarIcon || undefined;
  const avatarDataUrl =
    member.avatarDataUrl || agent?.avatarDataUrl || undefined;
  const offline = !!agent?.deviceID && agent.online === false;
  return (
    <li
      className={cn(
        "flex flex-col rounded-md border border-border bg-card px-2 py-1.5 text-xs",
        inherited && "opacity-70",
        offline && "opacity-65",
      )}
    >
      {/* Main row */}
      <div className="flex items-center gap-2">
        <AgentAvatar
          name={name}
          initials={name.charAt(0)}
          color={color}
          avatarIcon={avatarIcon}
          avatarDataUrl={avatarDataUrl}
          size="sm"
          className="size-5"
        />
        <span className="min-w-0 flex-1 truncate">{name}</span>
        <DeviceTag
          deviceId={agent?.deviceID ?? ""}
          deviceName={agent?.deviceName ?? ""}
          online={agent?.deviceID ? (agent.online ?? false) : true}
        />
        {inherited ? (
          <span
            className="rounded-sm bg-secondary px-1.5 py-0.5 text-2xs text-muted-foreground"
            title={`继承自 ${member.fromName ?? "父项目"}`}
          >
            ⋯ 继承
          </span>
        ) : (
          <button
            type="button"
            onClick={onRemove}
            disabled={busy}
            aria-label={`移除 ${name}`}
            className="inline-flex size-6 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-accent hover:text-destructive"
          >
            <Trash2 className="size-3" aria-hidden="true" />
          </button>
        )}
      </div>
      {/* cwd subrow — shown when a remote location is configured */}
      {location ? (
        <div className="mt-1 flex items-center gap-1.5 pl-7 text-2xs text-muted-foreground">
          <span className="font-mono text-subtle-foreground">cwd</span>
          <span className="min-w-0 flex-1 truncate font-mono">
            {location.path}
          </span>
        </div>
      ) : null}
    </li>
  );
}

function CandidateRow({
  agent,
  existingLocation,
  onAdd,
  busy,
}: {
  agent: ChatAgentItem;
  existingLocation?: ProjectLocationView;
  onAdd: () => Promise<void>;
  busy: boolean;
}) {
  // STATE: local agent → 1-click
  if (!agent.deviceID) {
    return (
      <button
        type="button"
        onClick={() => void onAdd()}
        disabled={busy}
        className="flex w-full items-center gap-2 rounded-md border border-border bg-card px-2 py-1.5 text-left text-xs transition-colors hover:bg-accent disabled:opacity-50"
      >
        <Avatar agent={agent} />
        <span className="min-w-0 flex-1 truncate">{agent.name}</span>
        <DeviceTag deviceId="" deviceName="" online />
        <Plus className="size-3 text-muted-foreground" aria-hidden="true" />
      </button>
    );
  }

  // STATE: remote agent + 该 device 已有 location → 1-click 加成员（cwd 由 chat_svc 自动解析）
  if (existingLocation) {
    return (
      <button
        type="button"
        onClick={() => void onAdd()}
        disabled={busy || agent.online === false}
        className={cn(
          "flex w-full items-center gap-2 rounded-md border border-border bg-card px-2 py-1.5 text-left text-xs transition-colors hover:bg-accent disabled:opacity-50",
          agent.online === false && "opacity-65",
        )}
      >
        <Avatar agent={agent} />
        <span className="min-w-0 flex-1 truncate">{agent.name}</span>
        <DeviceTag
          deviceId={agent.deviceID}
          deviceName={agent.deviceName ?? ""}
          online={agent.online ?? false}
        />
        <span
          className="truncate font-mono text-2xs text-muted-foreground"
          title={existingLocation.path}
        >
          {existingLocation.path}
        </span>
        <Plus className="size-3 text-muted-foreground" aria-hidden="true" />
      </button>
    );
  }

  // STATE: remote agent + 该 device 未配 location → 禁用 + 引导
  return (
    <div
      className="flex w-full cursor-not-allowed items-center gap-2 rounded-md border border-dashed border-border bg-card/40 px-2 py-1.5 text-xs opacity-65"
      title="该设备未配置远端路径"
    >
      <Avatar agent={agent} />
      <span className="min-w-0 flex-1 truncate">{agent.name}</span>
      <DeviceTag
        deviceId={agent.deviceID}
        deviceName={agent.deviceName ?? ""}
        online={agent.online ?? false}
      />
      <span className="text-2xs text-amber-600 dark:text-amber-500">
        ⚠ 先去「远端路径」配置
      </span>
    </div>
  );
}

function Avatar({ agent }: { agent: ChatAgentItem }) {
  return (
    <span
      className={cn(
        "inline-flex size-5 shrink-0 items-center justify-center rounded-full text-2xs font-semibold text-white",
        agentColorClassNames[(agent.avatarColor as AgentColor) || "agent-1"],
      )}
      aria-hidden="true"
    >
      {(agent.name ?? "?").charAt(0)}
    </span>
  );
}

function LocationsTab({ detail }: { detail: ProjectDetailResponse }) {
  const p = detail.project!;
  const [rows, setRows] = React.useState<ProjectLocationView[]>([]);
  const [devices, setDevices] = React.useState<DeviceView[]>([]);
  const [loading, setLoading] = React.useState(false);
  const [adding, setAdding] = React.useState(false);
  const [editingDevice, setEditingDevice] = React.useState<string | null>(null);
  const [err, setErr] = React.useState<string | null>(null);

  const reload = React.useCallback(async () => {
    setLoading(true);
    try {
      const [locs, devs] = await Promise.all([
        ProjectLocationList(p.id),
        RemoteDeviceList(),
      ]);
      setRows((locs ?? []) as ProjectLocationView[]);
      setDevices((devs ?? []) as DeviceView[]);
    } catch (e) {
      setErr(String(e));
    } finally {
      setLoading(false);
    }
  }, [p.id]);

  React.useEffect(() => {
    void reload();
  }, [reload]);

  const configuredDeviceIds = new Set(rows.map((r) => r.deviceId));
  const availableDevices = devices.filter(
    (d) => !configuredDeviceIds.has(String(d.id)),
  );

  const handleRemove = async (deviceId: string) => {
    setErr(null);
    try {
      await ProjectLocationRemove(p.id, deviceId);
      await reload();
    } catch (e) {
      setErr(String(e));
    }
  };

  return (
    <>
      <SectionLabel
        icon={<FolderTree className="size-3.5" aria-hidden="true" />}
        label={`远端路径 · ${rows.length}`}
      />
      {loading ? (
        <div className="flex items-center justify-center py-4 text-2xs text-muted-foreground">
          <Loader2 className="mr-1.5 size-3 animate-spin" aria-hidden="true" />
          加载中…
        </div>
      ) : rows.length === 0 ? (
        <EmptyHint text="还没有远端路径。加完路径后才能在「成员」tab 里添加该设备上的 Agent。" />
      ) : (
        <ul className="flex flex-col gap-1">
          {rows.map((r) =>
            editingDevice === r.deviceId ? (
              <LocationEditRow
                key={r.deviceId}
                projectId={p.id}
                deviceId={r.deviceId}
                deviceName={r.deviceName}
                online={r.online}
                initialPath={r.path}
                devices={devices}
                onCancel={() => setEditingDevice(null)}
                onSaved={async () => {
                  setEditingDevice(null);
                  await reload();
                }}
                onError={(msg) => setErr(msg)}
              />
            ) : (
              <LocationRow
                key={r.deviceId}
                row={r}
                onEdit={() => setEditingDevice(r.deviceId)}
                onRemove={() => void handleRemove(r.deviceId)}
              />
            ),
          )}
        </ul>
      )}

      <div className="border-t border-border pt-2">
        {adding ? (
          <LocationEditRow
            projectId={p.id}
            availableDevices={availableDevices}
            devices={devices}
            onCancel={() => setAdding(false)}
            onSaved={async () => {
              setAdding(false);
              await reload();
            }}
            onError={(msg) => setErr(msg)}
          />
        ) : (
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={availableDevices.length === 0}
            className="h-7 gap-1 self-start text-2xs"
            onClick={() => setAdding(true)}
            title={
              availableDevices.length === 0
                ? "所有已配对的远端设备都已配置路径"
                : undefined
            }
          >
            <Plus className="size-3.5" aria-hidden="true" />
            添加远端路径
          </Button>
        )}
      </div>
      {err ? (
        <div className="rounded-md border border-destructive bg-destructive-soft px-3 py-2 text-2xs text-destructive">
          {err}
        </div>
      ) : null}
    </>
  );
}

function LocationRow({
  row,
  onEdit,
  onRemove,
}: {
  row: ProjectLocationView;
  onEdit: () => void;
  onRemove: () => void;
}) {
  return (
    <li
      className={cn(
        "flex flex-col rounded-md border border-border bg-card px-2 py-1.5 text-xs",
        !row.online && "opacity-65",
      )}
    >
      <div className="flex items-center gap-2">
        <DeviceTag
          deviceId={row.deviceId}
          deviceName={row.deviceName}
          online={row.online}
        />
        <span className="min-w-0 flex-1" />
        <button
          type="button"
          onClick={onEdit}
          aria-label="编辑路径"
          className="inline-flex size-6 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
        >
          <Pencil className="size-3" aria-hidden="true" />
        </button>
        <button
          type="button"
          onClick={onRemove}
          aria-label="删除路径"
          className="inline-flex size-6 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-accent hover:text-destructive"
        >
          <Trash2 className="size-3" aria-hidden="true" />
        </button>
      </div>
      <div className="mt-1 truncate pl-1 font-mono text-2xs text-muted-foreground">
        {row.path}
      </div>
    </li>
  );
}

function LocationEditRow(props: {
  projectId: number;
  // Edit mode (existing row):
  deviceId?: string;
  deviceName?: string;
  online?: boolean;
  initialPath?: string;
  // Add mode:
  availableDevices?: DeviceView[];
  // NEW — used to look up name in add mode after a device is picked:
  devices?: DeviceView[];
  onCancel: () => void;
  onSaved: () => Promise<void> | void;
  onError: (msg: string) => void;
}) {
  const isEdit = !!props.deviceId;
  const [deviceId, setDeviceId] = React.useState<string>(
    props.deviceId ?? props.availableDevices?.[0]?.id?.toString() ?? "",
  );
  const [path, setPath] = React.useState<string>(props.initialPath ?? "");
  const [busy, setBusy] = React.useState(false);
  const [pickerOpen, setPickerOpen] = React.useState(false);

  const resolvedDeviceName =
    props.deviceName ??
    props.devices?.find((d) => String(d.id) === deviceId)?.name ??
    "";

  const pathValid = path.startsWith("/");
  const canSave = !!deviceId && pathValid && !busy;

  const handleSave = async () => {
    if (!canSave) return;
    setBusy(true);
    try {
      await ProjectLocationUpsert(props.projectId, deviceId, path);
      await props.onSaved();
    } catch (e) {
      props.onError(String(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="flex flex-col gap-1.5 rounded-md border border-primary bg-primary-soft p-2 text-2xs">
      <div className="flex items-center gap-2">
        {isEdit ? (
          <DeviceTag
            deviceId={props.deviceId!}
            deviceName={props.deviceName ?? ""}
            online={!!props.online}
          />
        ) : (
          <Select value={deviceId} onValueChange={setDeviceId}>
            <SelectTrigger
              aria-label="远端设备"
              className="h-7 min-w-[160px] text-2xs"
            >
              <SelectValue placeholder="选择远端设备" />
            </SelectTrigger>
            <SelectContent>
              {(props.availableDevices ?? []).map((d) => (
                <SelectItem
                  key={d.id}
                  value={String(d.id)}
                  disabled={!d.online}
                >
                  📡 {d.name}
                  {d.online ? "" : " · offline"}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
        <span className="ml-auto text-primary">
          {isEdit ? "编辑路径" : "新增路径"}
        </span>
      </div>
      <div className="flex items-center gap-1">
        <input
          value={path}
          onChange={(e) => setPath(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && canSave) void handleSave();
          }}
          className="flex-1 rounded-sm border border-border bg-background px-2 py-1 font-mono"
          placeholder="/abs/path on remote (e.g. /home/me/proj)"
          autoFocus
        />
        <button
          type="button"
          onClick={() => setPickerOpen(true)}
          disabled={!deviceId}
          aria-label="浏览远端目录"
          title="浏览远端目录"
          className="inline-flex size-7 items-center justify-center rounded text-muted-foreground hover:bg-accent hover:text-foreground disabled:opacity-50"
        >
          <Folder className="size-3.5" />
        </button>
      </div>
      {deviceId ? (
        <RemoteFsPicker
          open={pickerOpen}
          onOpenChange={setPickerOpen}
          deviceID={deviceId}
          deviceName={resolvedDeviceName}
          mode="dir"
          initialPath={path || undefined}
          onPick={(picked) => setPath(picked)}
        />
      ) : null}
      <div className="flex items-center justify-between">
        <span className="text-2xs text-subtle-foreground">
          {pathValid || path === "" ? "Enter 保存" : "必须是绝对路径 (/开头)"}
        </span>
        <div className="flex gap-1.5">
          <button
            type="button"
            onClick={props.onCancel}
            className="text-muted-foreground"
          >
            <X className="size-3.5" aria-hidden="true" />
          </button>
          <button
            type="button"
            onClick={() => void handleSave()}
            disabled={!canSave}
            className="rounded-sm bg-primary px-2 py-1 text-primary-foreground disabled:opacity-50"
          >
            {busy ? (
              <Loader2 className="size-3 animate-spin" aria-hidden="true" />
            ) : (
              "保存"
            )}
          </button>
        </div>
      </div>
    </div>
  );
}

function DangerTab({
  detail,
  onDeleted,
}: {
  detail: ProjectDetailResponse;
  onDeleted: () => void;
}) {
  const p = detail.project!;
  const [confirm, setConfirm] = React.useState("");
  const [err, setErr] = React.useState<string | null>(null);
  const [busy, setBusy] = React.useState(false);

  const canDelete = confirm.trim() === p.name && !busy;

  const handleDelete = async () => {
    setErr(null);
    setBusy(true);
    try {
      await ProjectDelete(p.id);
      onDeleted();
    } catch (e) {
      setErr(String(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <div className="flex items-start gap-2 rounded-md border border-destructive/30 bg-destructive-soft px-3 py-2.5 text-2xs text-destructive">
        <AlertTriangle className="mt-0.5 size-3.5" aria-hidden="true" />
        <div className="flex flex-col gap-1">
          <span className="font-semibold">删除项目</span>
          <span>
            软删除项目记录，**不会**删除本地代码仓库或会话内容。如果项目还有子项目或活跃会话，删除会被拒绝。
          </span>
        </div>
      </div>
      <Field label={`输入项目名 "${p.name}" 以确认`}>
        <Input
          value={confirm}
          onChange={(e) => setConfirm(e.target.value)}
          className="h-9 font-mono text-xs"
          placeholder={p.name}
        />
      </Field>
      {err ? (
        <div className="rounded-md border border-destructive bg-destructive-soft px-3 py-2 text-2xs text-destructive">
          {err}
        </div>
      ) : null}
      <div className="mt-1 flex justify-end">
        <Button
          type="button"
          variant="destructive"
          disabled={!canDelete}
          onClick={() => void handleDelete()}
        >
          {busy ? (
            <Loader2 className="size-3.5 animate-spin" aria-hidden="true" />
          ) : (
            <Trash2 className="size-3.5" aria-hidden="true" />
          )}
          删除项目
        </Button>
      </div>
    </>
  );
}

// ─── Field helpers ───────────────────────────────────────────────────────────

function Field({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <label className="flex flex-col gap-1.5 text-xs">
      <span className="font-medium text-foreground">{label}</span>
      {children}
    </label>
  );
}

function SectionLabel({
  icon,
  label,
}: {
  icon?: React.ReactNode;
  label: string;
}) {
  return (
    <div className="flex items-center gap-1.5 font-mono text-2xs font-semibold uppercase text-subtle-foreground">
      {icon}
      {label}
    </div>
  );
}

function EmptyHint({ text }: { text: string }) {
  return (
    <div className="rounded-md border border-dashed border-border bg-card/40 px-3 py-3 text-center text-2xs text-muted-foreground">
      {text}
    </div>
  );
}

export { ProjectSettingsDrawer };
