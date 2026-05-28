import { vi } from "vitest";

type AnyFn = (...args: unknown[]) => unknown;

function windowBackedMock(
  name: string,
  fallback: AnyFn,
): ReturnType<typeof vi.fn> {
  const mock: ReturnType<typeof vi.fn> = vi.fn((...args: unknown[]) => {
    const fn = window.go?.app?.App?.[name];
    if (typeof fn === "function" && fn !== mock) return fn(...args);
    return fallback(...args);
  });
  return mock;
}

export const Greet = vi.fn((name: string) =>
  Promise.resolve(`Hello ${name}, It's show time!`),
);

export const AnswerUserQuestion = vi.fn(() => Promise.resolve({}));
export const AnswerToolPermission = vi.fn(() => Promise.resolve({}));
export const ResolvePlanAction = vi.fn(() => Promise.resolve({}));

export const Info = vi.fn(() =>
  Promise.resolve({
    version: "dev",
    commit: "dev",
    builtAt: "",
  }),
);

// LLM provider bindings — used by llm-providers.tsx and agent-backends.tsx.
// Tests override these via window.go.app.App.* or vi.mock at test level.
export const ListLLMProviders = windowBackedMock("ListLLMProviders", () =>
  Promise.resolve({ items: [] }),
);
export const CreateLLMProvider = windowBackedMock("CreateLLMProvider", () =>
  Promise.resolve({ item: { id: 1 } }),
);
export const UpdateLLMProvider = windowBackedMock("UpdateLLMProvider", () =>
  Promise.resolve({ item: { id: 1 } }),
);
export const DeleteLLMProvider = windowBackedMock("DeleteLLMProvider", () =>
  Promise.resolve({}),
);
export const ListLLMModels = windowBackedMock("ListLLMModels", () =>
  Promise.resolve({ items: [] }),
);
export const PreviewLLMModels = windowBackedMock("PreviewLLMModels", () =>
  Promise.resolve({ items: [] }),
);
export const TestLLMProvider = windowBackedMock("TestLLMProvider", () =>
  Promise.resolve({ ok: true, message: "", modelCount: 0 }),
);
export const LookupLLMModel = windowBackedMock("LookupLLMModel", () =>
  Promise.resolve({ known: false, vendor: "", contextWindow: 0, maxOutput: 0 }),
);

// Agent backend bindings
export const ListAgentBackends = windowBackedMock("ListAgentBackends", () =>
  Promise.resolve({ items: [] }),
);
export const CreateAgentBackend = windowBackedMock("CreateAgentBackend", () =>
  Promise.resolve({ item: { id: 1 } }),
);
export const UpdateAgentBackend = windowBackedMock("UpdateAgentBackend", () =>
  Promise.resolve({ item: { id: 1 } }),
);
export const DeleteAgentBackend = windowBackedMock("DeleteAgentBackend", () =>
  Promise.resolve({}),
);
export const TestAgentBackend = windowBackedMock("TestAgentBackend", () =>
  Promise.resolve({ ok: true, latencyMs: 0, message: "" }),
);
export const CancelTestAgentBackend = windowBackedMock(
  "CancelTestAgentBackend",
  () => Promise.resolve({ canceled: true }),
);
export const ResolveAgentBackendCLIPath = windowBackedMock(
  "ResolveAgentBackendCLIPath",
  () => Promise.resolve({ path: "", found: false }),
);
export const GetGatewayStatus = windowBackedMock("GetGatewayStatus", () =>
  Promise.resolve({ status: "stopped", listenURL: "", reason: "", routes: [] }),
);

// Organization bindings
export const LoadOrg = windowBackedMock("LoadOrg", () =>
  Promise.resolve({ departments: [], agents: [] }),
);
export const CreateDepartment = windowBackedMock("CreateDepartment", () =>
  Promise.resolve({ item: {} }),
);
export const UpdateDepartment = windowBackedMock("UpdateDepartment", () =>
  Promise.resolve({ item: {} }),
);
export const MoveDepartment = windowBackedMock("MoveDepartment", () =>
  Promise.resolve({ item: {} }),
);
export const DeleteDepartment = windowBackedMock("DeleteDepartment", () =>
  Promise.resolve({}),
);
export const CreateAgent = windowBackedMock("CreateAgent", () =>
  Promise.resolve({ item: {} }),
);
export const UpdateAgent = windowBackedMock("UpdateAgent", () =>
  Promise.resolve({ item: {} }),
);
export const MoveAgent = windowBackedMock("MoveAgent", () =>
  Promise.resolve({ item: {} }),
);
export const DeleteAgent = windowBackedMock("DeleteAgent", () =>
  Promise.resolve({}),
);
export const UploadAgentAvatar = windowBackedMock("UploadAgentAvatar", () =>
  Promise.resolve({ item: {} }),
);
export const DeleteAgentAvatar = windowBackedMock("DeleteAgentAvatar", () =>
  Promise.resolve({}),
);

// Chat and project bindings
export const ListChatAgents = windowBackedMock("ListChatAgents", () =>
  Promise.resolve({ agents: [] }),
);
export const ListChatAgentSessions = windowBackedMock(
  "ListChatAgentSessions",
  () => Promise.resolve({ sessions: [] }),
);
export const LoadChatSession = windowBackedMock("LoadChatSession", () =>
  Promise.resolve({ session: null, messages: [] }),
);
export const MarkChatSessionRead = windowBackedMock("MarkChatSessionRead", () =>
  Promise.resolve({}),
);
export const ProjectListTree = windowBackedMock("ProjectListTree", () =>
  Promise.resolve([]),
);
export const ProjectGet = windowBackedMock("ProjectGet", () =>
  Promise.resolve({ item: null }),
);
export const ProjectListSessions = windowBackedMock("ProjectListSessions", () =>
  Promise.resolve([]),
);
export const ProjectLocationList = windowBackedMock("ProjectLocationList", () =>
  Promise.resolve([]),
);
export const ProjectCreate = windowBackedMock("ProjectCreate", () =>
  Promise.resolve({ item: { id: 1 } }),
);
export const ProjectUpdate = windowBackedMock("ProjectUpdate", () =>
  Promise.resolve({ item: { id: 1 } }),
);
export const ProjectDelete = windowBackedMock("ProjectDelete", () =>
  Promise.resolve({}),
);
export const ProjectReorder = windowBackedMock("ProjectReorder", () =>
  Promise.resolve({}),
);
export const ProjectAddMember = windowBackedMock("ProjectAddMember", () =>
  Promise.resolve({}),
);
export const ProjectRemoveMember = windowBackedMock("ProjectRemoveMember", () =>
  Promise.resolve({}),
);
export const ProjectMove = windowBackedMock("ProjectMove", () =>
  Promise.resolve({ item: { id: 1 } }),
);

export const GetSessionCapabilities = windowBackedMock(
  "GetSessionCapabilities",
  () => Promise.resolve({ capabilities: [], permissionModeMeta: null }),
);
export const GetBackendCapabilities = windowBackedMock(
  "GetBackendCapabilities",
  () => Promise.resolve({ capabilities: [], permissionModeMeta: null }),
);
export const SetChatPermissionMode = windowBackedMock(
  "SetChatPermissionMode",
  () => Promise.resolve({}),
);

export const RemoteDeviceList = windowBackedMock("RemoteDeviceList", () =>
  Promise.resolve([]),
);
export const RemoteFsListDir = windowBackedMock("RemoteFsListDir", () =>
  Promise.resolve({ entries: [] }),
);
export const RemoteFsMkdir = windowBackedMock("RemoteFsMkdir", () =>
  Promise.resolve({}),
);

// Data backup bindings
export const ExportData = windowBackedMock("ExportData", () =>
  Promise.resolve({ path: "", canceled: true, summary: {} }),
);
export const PreviewImportData = windowBackedMock("PreviewImportData", () =>
  Promise.resolve({
    format: "",
    version: 0,
    secretsIncluded: false,
    items: [],
  }),
);
export const ApplyImportData = windowBackedMock("ApplyImportData", () =>
  Promise.resolve({ counts: {} }),
);
