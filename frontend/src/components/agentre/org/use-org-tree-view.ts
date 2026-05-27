import * as React from "react";

import type { OrgSelection } from "./types";

const KEY_COLLAPSE = "agentre.orgTree.collapse";
const KEY_ZOOM = "agentre.orgTree.zoom";
const KEY_PAN = "agentre.orgTree.pan.v2";
const KEY_SELECTED = "agentre.orgTree.selected";
const KEY_VIEW_MODE = "agentre.orgView.mode";

export type OrgViewMode = "tree" | "list";

const MIN_ZOOM = 0.6;
const MAX_ZOOM = 1.6;
const ZOOM_STEP = 0.1;

function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}

function safeParse<T>(raw: string | null, fallback: T): T {
  if (!raw) return fallback;
  try {
    return JSON.parse(raw) as T;
  } catch {
    return fallback;
  }
}

type Pan = { x: number; y: number };

export function useOrgTreeView() {
  const [collapse, setCollapse] = React.useState<Record<number, boolean>>(() =>
    safeParse<Record<number, boolean>>(localStorage.getItem(KEY_COLLAPSE), {}),
  );
  const [zoom, setZoom] = React.useState<number>(() => {
    const stored = safeParse<number | null>(
      localStorage.getItem(KEY_ZOOM),
      null,
    );
    return stored === null ? 1 : clamp(stored, MIN_ZOOM, MAX_ZOOM);
  });
  const [pan, setPan] = React.useState<Pan>(() =>
    safeParse<Pan>(localStorage.getItem(KEY_PAN), { x: 0, y: 0 }),
  );
  const [selected, setSelectedRaw] = React.useState<OrgSelection>(() =>
    safeParse<OrgSelection>(localStorage.getItem(KEY_SELECTED), null),
  );
  const [viewMode, setViewModeRaw] = React.useState<OrgViewMode>(() => {
    const stored = localStorage.getItem(KEY_VIEW_MODE);
    return stored === "list" || stored === "tree" ? stored : "tree";
  });

  React.useEffect(() => {
    localStorage.setItem(KEY_COLLAPSE, JSON.stringify(collapse));
  }, [collapse]);
  React.useEffect(() => {
    localStorage.setItem(KEY_ZOOM, JSON.stringify(zoom));
  }, [zoom]);
  React.useEffect(() => {
    localStorage.setItem(KEY_PAN, JSON.stringify(pan));
  }, [pan]);
  React.useEffect(() => {
    localStorage.setItem(KEY_SELECTED, JSON.stringify(selected));
  }, [selected]);
  React.useEffect(() => {
    localStorage.setItem(KEY_VIEW_MODE, viewMode);
  }, [viewMode]);

  const toggleCollapse = React.useCallback((id: number) => {
    setCollapse((s) => ({ ...s, [id]: !s[id] }));
  }, []);
  const zoomIn = React.useCallback(
    () => setZoom((z) => clamp(z + ZOOM_STEP, MIN_ZOOM, MAX_ZOOM)),
    [],
  );
  const zoomOut = React.useCallback(
    () => setZoom((z) => clamp(z - ZOOM_STEP, MIN_ZOOM, MAX_ZOOM)),
    [],
  );
  const zoomReset = React.useCallback(() => {
    setZoom(1);
    setPan({ x: 0, y: 0 });
  }, []);
  const setSelected = React.useCallback(
    (sel: OrgSelection) => setSelectedRaw(sel),
    [],
  );
  const setViewMode = React.useCallback(
    (mode: OrgViewMode) => setViewModeRaw(mode),
    [],
  );

  return {
    collapse,
    toggleCollapse,
    zoom,
    setZoom: (v: number) => setZoom(clamp(v, MIN_ZOOM, MAX_ZOOM)),
    zoomIn,
    zoomOut,
    zoomReset,
    pan,
    setPan,
    selected,
    setSelected,
    viewMode,
    setViewMode,
  };
}

export const ORG_TREE_VIEW_TEST_KEYS = {
  collapse: KEY_COLLAPSE,
  zoom: KEY_ZOOM,
  pan: KEY_PAN,
  selected: KEY_SELECTED,
  viewMode: KEY_VIEW_MODE,
};
