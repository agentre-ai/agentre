import * as React from "react";

export type AutoSaveStatus = "idle" | "saving" | "saved" | "error";

export interface UseAutoSaveOptions<T> {
  initial: T;
  save: (values: T) => Promise<unknown>;
  debounceMs?: number;
  isValid?: (values: T) => boolean;
}

export interface UseAutoSaveResult<T> {
  values: T;
  patch: (partial: Partial<T>, opts?: { immediate?: boolean }) => void;
  flush: () => void;
  wrap: <R>(fn: () => Promise<R>) => Promise<R | null>;
  status: AutoSaveStatus;
  pendingInvalid: boolean;
  retry: () => void;
}

export function useAutoSave<T extends object>(
  opts: UseAutoSaveOptions<T>,
): UseAutoSaveResult<T> {
  const debounceMs = opts.debounceMs ?? 600;

  const [values, setValues] = React.useState<T>(opts.initial);
  const [status, setStatus] = React.useState<AutoSaveStatus>("idle");
  const [pendingInvalid, setPendingInvalid] = React.useState(false);

  // refs 让所有回调保持稳定身份,并且保存时读到最新值/最新 save/isValid。
  const valuesRef = React.useRef(values);
  const saveRef = React.useRef(opts.save);
  const isValidRef = React.useRef(opts.isValid);
  const timerRef = React.useRef<ReturnType<typeof setTimeout> | null>(null);
  const lastActionRef = React.useRef<(() => Promise<unknown>) | null>(null);

  // Sync refs after render using useLayoutEffect so they're available in callbacks
  React.useLayoutEffect(() => {
    valuesRef.current = values;
    saveRef.current = opts.save;
    isValidRef.current = opts.isValid;
  });

  const clearTimer = React.useCallback(() => {
    if (timerRef.current !== null) {
      clearTimeout(timerRef.current);
      timerRef.current = null;
    }
  }, []);

  const run = React.useCallback(async (action: () => Promise<unknown>) => {
    lastActionRef.current = action;
    setStatus("saving");
    try {
      await action();
      setStatus("saved");
    } catch {
      setStatus("error");
    }
  }, []);

  const saveNow = React.useCallback(() => {
    clearTimer();
    const snapshot = valuesRef.current;
    void run(() => saveRef.current(snapshot));
  }, [clearTimer, run]);

  const patch = React.useCallback(
    (partial: Partial<T>, patchOpts?: { immediate?: boolean }) => {
      const next = { ...valuesRef.current, ...partial };
      valuesRef.current = next;
      setValues(next);

      const isValid = isValidRef.current;
      if (isValid && !isValid(next)) {
        clearTimer();
        setPendingInvalid(true);
        return;
      }
      setPendingInvalid(false);

      if (patchOpts?.immediate) {
        saveNow();
      } else {
        clearTimer();
        timerRef.current = setTimeout(saveNow, debounceMs);
      }
    },
    [clearTimer, debounceMs, saveNow],
  );

  const flush = React.useCallback(() => {
    if (timerRef.current !== null) {
      saveNow();
    }
  }, [saveNow]);

  const wrap = React.useCallback(
    async <R>(fn: () => Promise<R>): Promise<R | null> => {
      let result: R | null = null;
      await run(async () => {
        result = await fn();
      });
      return result;
    },
    [run],
  );

  const retry = React.useCallback(() => {
    const action = lastActionRef.current;
    if (action) void run(action);
  }, [run]);

  React.useEffect(() => () => clearTimer(), [clearTimer]);

  return { values, patch, flush, wrap, status, pendingInvalid, retry };
}
