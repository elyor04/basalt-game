// Procedural sound: every effect is synthesised with WebAudio, so the game
// ships no audio files. Stone clacks are short filtered noise bursts over a
// low thump; captures add a rumble and a splash.

export type Sfx = "place" | "push" | "fall" | "collapse" | "ui" | "back" | "win" | "loss" | "hint" | "deny" | "select";

type Ctx = AudioContext;

export class AudioEngine {
  private ctx: Ctx | null = null;
  private master: GainNode | null = null;
  private noise: AudioBuffer | null = null;
  private volume = 0.7;

  setVolume(v: number) {
    this.volume = Math.max(0, Math.min(1, v));
    if (this.master && this.ctx) this.master.gain.setTargetAtTime(this.volume * 0.9, this.ctx.currentTime, 0.02);
  }

  /** Create or wake the context; call from a user gesture. */
  unlock() {
    try {
      if (!this.ctx) {
        const AC = window.AudioContext ?? (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
        if (!AC) return;
        this.ctx = new AC();
        this.master = this.ctx.createGain();
        this.master.gain.value = this.volume * 0.9;
        this.master.connect(this.ctx.destination);
        const len = Math.floor(this.ctx.sampleRate * 1.2);
        this.noise = this.ctx.createBuffer(1, len, this.ctx.sampleRate);
        const d = this.noise.getChannelData(0);
        for (let i = 0; i < len; i++) d[i] = Math.random() * 2 - 1;
      }
      this.resume();
    } catch {
      /* audio unavailable: play silently */
    }
  }

  /** iOS reports "interrupted"; anything but running/closed needs a resume. */
  resume() {
    const c = this.ctx;
    if (c && c.state !== "running" && (c.state as string) !== "closed") void c.resume().catch(() => {});
  }

  suspend() {
    if (this.ctx?.state === "running") void this.ctx.suspend().catch(() => {});
  }

  play(kind: Sfx, intensity = 1) {
    const c = this.ctx;
    if (!c || !this.master || this.volume <= 0) return;
    this.resume();
    const t = c.currentTime + 0.005;
    switch (kind) {
      case "place":
        this.thump(t, 150, 0.09, 0.5);
        this.burst(t, 2400, 9, 0.035, 0.45);
        break;
      case "push":
        this.burst(t, 900 + Math.random() * 300, 3, 0.09, 0.22 * intensity, 1400);
        break;
      case "fall":
        this.burst(t, 220, 0.8, 0.6, 0.35, 120);
        this.burst(t + 0.18, 3000, 1.2, 0.45, 0.16, 5200, "highpass");
        break;
      case "collapse":
        this.thump(t, 52, 0.7, 0.55);
        this.burst(t, 160, 0.7, 0.9, 0.3, 90);
        break;
      case "ui":
        this.tone(t, 660, 0.05, 0.08, "triangle");
        break;
      case "select":
        this.tone(t, 520, 0.04, 0.06, "triangle");
        break;
      case "back":
        this.tone(t, 440, 0.06, 0.07, "triangle");
        break;
      case "hint":
        this.tone(t, 784, 0.25, 0.08, "sine");
        this.tone(t + 0.08, 1175, 0.3, 0.06, "sine");
        break;
      case "deny":
        this.thump(t, 90, 0.12, 0.35);
        break;
      case "win":
        [523, 659, 784, 1047].forEach((f, i) => this.tone(t + i * 0.11, f, 0.6, 0.1, "sine"));
        break;
      case "loss":
        [392, 311].forEach((f, i) => this.tone(t + i * 0.22, f, 0.7, 0.1, "triangle"));
        break;
    }
  }

  private out(gain: number, t: number, dur: number): GainNode {
    const c = this.ctx!;
    const g = c.createGain();
    g.gain.setValueAtTime(0.0001, t);
    g.gain.exponentialRampToValueAtTime(Math.max(gain, 0.0002), t + 0.005);
    g.gain.exponentialRampToValueAtTime(0.0001, t + dur);
    g.connect(this.master!);
    return g;
  }

  private thump(t: number, freq: number, dur: number, gain: number) {
    const c = this.ctx!;
    const o = c.createOscillator();
    o.type = "sine";
    o.frequency.setValueAtTime(freq * 1.6, t);
    o.frequency.exponentialRampToValueAtTime(freq, t + dur * 0.4);
    o.connect(this.out(gain, t, dur));
    o.start(t);
    o.stop(t + dur + 0.05);
  }

  private burst(t: number, freq: number, q: number, dur: number, gain: number, endFreq?: number, type: BiquadFilterType = "bandpass") {
    const c = this.ctx!;
    const src = c.createBufferSource();
    src.buffer = this.noise;
    src.playbackRate.value = 0.9 + Math.random() * 0.2;
    const f = c.createBiquadFilter();
    f.type = type;
    f.frequency.setValueAtTime(freq, t);
    if (endFreq) f.frequency.exponentialRampToValueAtTime(endFreq, t + dur);
    f.Q.value = q;
    src.connect(f);
    f.connect(this.out(gain, t, dur));
    src.start(t, Math.random() * 0.5);
    src.stop(t + dur + 0.05);
  }

  private tone(t: number, freq: number, dur: number, gain: number, type: OscillatorType) {
    const c = this.ctx!;
    const o = c.createOscillator();
    o.type = type;
    o.frequency.value = freq;
    o.connect(this.out(gain, t, dur));
    o.start(t);
    o.stop(t + dur + 0.05);
  }
}
