import { describe, it, expect, beforeEach } from 'vitest';
import { useChatTerminalStore } from '../chat-terminal-store';

describe('useChatTerminalStore', () => {
  beforeEach(() => {
    useChatTerminalStore.setState({ openSessionID: null });
  });

  it('defaults to null', () => {
    expect(useChatTerminalStore.getState().openSessionID).toBeNull();
  });

  it('toggle(7) opens session 7', () => {
    useChatTerminalStore.getState().toggle(7);
    expect(useChatTerminalStore.getState().openSessionID).toBe(7);
  });

  it('toggle(7) twice closes', () => {
    useChatTerminalStore.getState().toggle(7);
    useChatTerminalStore.getState().toggle(7);
    expect(useChatTerminalStore.getState().openSessionID).toBeNull();
  });

  it('toggle(8) while 7 is open switches to 8', () => {
    useChatTerminalStore.getState().toggle(7);
    useChatTerminalStore.getState().toggle(8);
    expect(useChatTerminalStore.getState().openSessionID).toBe(8);
  });
});
