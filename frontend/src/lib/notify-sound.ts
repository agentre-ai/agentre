export type SoundPreset = "ding" | "chime" | "blip";

export const SOUND_PRESETS: SoundPreset[] = ["ding", "chime", "blip"];

let ctx: AudioContext | null = null;

function audioContext(): AudioContext | null {
  try {
    const Ctor =
      (globalThis as { AudioContext?: typeof AudioContext }).AudioContext ??
      (globalThis as { webkitAudioContext?: typeof AudioContext })
        .webkitAudioContext;
    if (!Ctor) return null;
    ctx = ctx ?? new Ctor();
    return ctx;
  } catch {
    return null;
  }
}

function tone(
  c: AudioContext,
  freq: number,
  start: number,
  dur: number,
  peak: number,
): void {
  const osc = c.createOscillator();
  const gain = c.createGain();
  osc.type = "sine";
  osc.frequency.value = freq;
  gain.gain.setValueAtTime(0, start);
  gain.gain.linearRampToValueAtTime(peak, start + 0.01);
  gain.gain.exponentialRampToValueAtTime(0.0001, start + dur);
  osc.connect(gain);
  gain.connect(c.destination);
  osc.start(start);
  osc.stop(start + dur + 0.02);
}

// 各预设的音符表 [频率Hz, 相对起始秒, 时长秒]。
const RECIPES: Record<SoundPreset, [number, number, number][]> = {
  ding: [
    [880, 0, 0.45],
    [1320, 0.04, 0.45],
  ],
  chime: [
    [659, 0, 0.4],
    [880, 0.09, 0.4],
    [1175, 0.18, 0.5],
  ],
  blip: [[440, 0, 0.16]],
};

export function playNotifySound(preset: SoundPreset): void {
  const c = audioContext();
  if (!c) return;
  const t0 = c.currentTime;
  for (const [freq, offset, dur] of RECIPES[preset] ?? RECIPES.ding) {
    tone(c, freq, t0 + offset, dur, 0.18);
  }
}
