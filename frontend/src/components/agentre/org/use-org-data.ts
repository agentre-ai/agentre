import * as React from "react";

import i18n from "@/i18n";

import {
  CreateAgent,
  CreateDepartment,
  DeleteAgent,
  DeleteAgentAvatar,
  DeleteDepartment,
  ListAgentBackends,
  LoadOrg,
  MoveAgent,
  MoveDepartment,
  UpdateAgent,
  UpdateDepartment,
  UploadAgentAvatar,
} from "../../../../wailsjs/go/app/App";
import type {
  agent_backend_svc,
  agent_svc,
  department_svc,
} from "../../../../wailsjs/go/models";

import type { OrgAgent, OrgDepartment } from "./types";

type State = {
  loading: boolean;
  error: string | null;
  departments: OrgDepartment[];
  agents: OrgAgent[];
  backends: agent_backend_svc.BackendItem[];
};

const initialState: State = {
  loading: true,
  error: null,
  departments: [],
  agents: [],
  backends: [],
};

export function useOrgData() {
  const [state, setState] = React.useState<State>(initialState);
  const inFlight = React.useRef(0);

  const reload = React.useCallback(async () => {
    try {
      const [res, backendsRes] = await Promise.all([
        LoadOrg(),
        ListAgentBackends(),
      ]);
      setState({
        loading: false,
        error: null,
        departments: res.departments ?? [],
        agents: res.agents ?? [],
        backends: backendsRes.items ?? [],
      });
    } catch (err) {
      setState((s) => ({ ...s, loading: false, error: messageOf(err) }));
    }
  }, []);

  React.useEffect(() => {
    setState((s) => ({ ...s, loading: true }));
    void reload();
  }, [reload]);

  const mutate = React.useCallback(
    async <T>(fn: () => Promise<T>): Promise<T | null> => {
      inFlight.current += 1;
      try {
        const result = await fn();
        return result;
      } catch (err) {
        setState((s) => ({ ...s, error: messageOf(err) }));
        return null;
      } finally {
        inFlight.current -= 1;
        if (inFlight.current === 0) {
          void reload();
        }
      }
    },
    [reload],
  );

  return {
    ...state,
    reload,
    createDepartment: (req: department_svc.CreateDepartmentRequest) =>
      mutate(() => CreateDepartment(req)),
    updateDepartment: (req: department_svc.UpdateDepartmentRequest) =>
      mutate(() => UpdateDepartment(req)),
    moveDepartment: (req: department_svc.MoveDepartmentRequest) =>
      mutate(() => MoveDepartment(req)),
    deleteDepartment: (req: department_svc.DeleteDepartmentRequest) =>
      mutate(() => DeleteDepartment(req)),
    createAgent: (req: agent_svc.CreateAgentRequest) =>
      mutate(() => CreateAgent(req)),
    updateAgent: (req: agent_svc.UpdateAgentRequest) =>
      mutate(() => UpdateAgent(req)),
    moveAgent: (req: agent_svc.MoveAgentRequest) =>
      mutate(() => MoveAgent(req)),
    deleteAgent: (req: agent_svc.DeleteAgentRequest) =>
      mutate(() => DeleteAgent(req)),
    uploadAgentAvatar: (req: agent_svc.UploadAvatarRequest) =>
      mutate(() => UploadAgentAvatar(req)),
    deleteAgentAvatar: (req: agent_svc.DeleteAvatarRequest) =>
      mutate(() => DeleteAgentAvatar(req)),
  };
}

function messageOf(err: unknown): string {
  if (err instanceof Error) return err.message;
  if (typeof err === "string") return err;
  try {
    return JSON.stringify(err);
  } catch {
    return i18n.t("common.operationFailed");
  }
}
