import { afterEach, describe, expect, it, vi } from "vitest";
import { SOUND_PRESETS, playNotifySound } from "../notify-sound";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("notify-sound", () => {
  it("暴露三个预设", () => {
    expect(SOUND_PRESETS).toEqual(["ding", "chime", "blip"]);
  });

  it("无 AudioContext 时安全 no-op,不抛错", () => {
    vi.stubGlobal("AudioContext", undefined);
    vi.stubGlobal("webkitAudioContext", undefined);
    expect(() => playNotifySound("ding")).not.toThrow();
  });

  it("有 AudioContext 时为 chime 调度多个振荡器", () => {
    const starts: number[] = [];
    const fakeOsc = () => ({
      type: "",
      frequency: { value: 0 },
      connect: vi.fn(),
      start: (t: number) => starts.push(t),
      stop: vi.fn(),
    });
    const fakeGain = () => ({
      gain: {
        setValueAtTime: vi.fn(),
        linearRampToValueAtTime: vi.fn(),
        exponentialRampToValueAtTime: vi.fn(),
      },
      connect: vi.fn(),
    });
    class FakeCtx {
      currentTime = 0;
      destination = {};
      createOscillator() {
        return fakeOsc();
      }
      createGain() {
        return fakeGain();
      }
    }
    vi.stubGlobal("AudioContext", FakeCtx);
    playNotifySound("chime");
    expect(starts.length).toBeGreaterThanOrEqual(3); // chime = 三音琶音
  });
});
