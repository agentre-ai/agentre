type Listener = () => void;

let pending = false;
const listeners = new Set<Listener>();

export function requestNewAgentDialog(): void {
  pending = true;
  for (const listener of listeners) listener();
}

export function consumeNewAgentDialogIntent(): boolean {
  if (!pending) return false;
  pending = false;
  return true;
}

export function subscribeNewAgentIntent(listener: Listener): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}
