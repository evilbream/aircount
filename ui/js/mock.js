// Offline mode (?mock=1): stands in for the socket and emits exactly what
// api-service emits — one flat CSI window per message, all sensors mixed on one
// connection. Lets the UI be worked on without the backend, and afterwards
// serves as the fixture when frontend and backend disagree.
//
//   ?mock=1          live only, one window per sensor every 5s
//   ?mock=1&seed=1   plus ~2h of backdated points, to exercise 6h/24h at once
//   ?mock=1&flaky=1  plus a dropped connection at 45s

import { NATIVE_WINDOW_MS } from './series.js';

const SENSORS = ['living-room', 'kitchen', 'hallway'];
const SEED_SPAN_MS = 2 * 60 * 60_000;

function hash01(str) {
  let h = 0x811c9dc5;
  for (let i = 0; i < str.length; i++) {
    h ^= str.charCodeAt(i);
    h = Math.imul(h, 0x01000193) >>> 0;
  }
  return (h >>> 8) / 0x01000000;
}

/**
 * Deterministic synthetic motion_score for one sensor at one 5s window, shaped
 * after the real testdata captures: a quiet floor around 0.005-0.03, activity
 * bursts above the 0.06 threshold, and rare spikes near 20 that are what the
 * log axis exists for.
 */
function scoreAt(sensorId, tMs) {
  const slot = Math.floor(tMs / NATIVE_WINDOW_MS) * NATIVE_WINDOW_MS;
  const r = hash01(`${sensorId}:${slot}`);
  if (r < 0.02) return null; // silent window (packets == 0 is never published)

  const phase = hash01(`${sensorId}:phase:${Math.floor(tMs / 600_000)}`);
  const active = phase > 0.62;
  let v = active ? 0.05 + r * 0.42 : 0.004 + r * 0.028;
  if (r > 0.995) v *= 40;
  return v;
}

/** Minimal WebSocket look-alike: only what ws.js actually touches. */
class MockSocket {
  constructor(url, { flaky = false, seed = false } = {}) {
    this.url = url;
    this.readyState = 0; // CONNECTING
    this.onopen = this.onmessage = this.onclose = this.onerror = null;
    this.timers = [];
    this.flaky = flaky;
    this.seed = seed;
    setTimeout(() => this.#open(), 150);
  }

  #emit(sensorID, tMs, score) {
    if (this.readyState !== 1 || !this.onmessage) return;
    this.onmessage({
      data: JSON.stringify({ sensor_id: sensorID, timestamp: tMs, motion_score: score }),
    });
  }

  #open() {
    this.readyState = 1;
    this.onopen?.({});

    if (this.seed) {
      const now = Date.now();
      for (let t = now - SEED_SPAN_MS; t <= now; t += NATIVE_WINDOW_MS) {
        for (const id of SENSORS) {
          const v = scoreAt(id, t);
          if (v != null) this.#emit(id, t, v);
        }
      }
    }

    this.timers.push(setInterval(() => {
      const now = Date.now();
      for (const id of SENSORS) {
        const v = scoreAt(id, now);
        if (v != null) this.#emit(id, now, v);
      }
    }, NATIVE_WINDOW_MS));

    // Drop once, mid-session, so the reconnect path gets exercised for real.
    if (this.flaky) this.timers.push(setTimeout(() => this.#drop(), 45_000));
  }

  #drop() {
    if (this.readyState !== 1) return;
    this.readyState = 3;
    this.#clear();
    this.onclose?.({ code: 1006, reason: 'mock drop', wasClean: false });
  }

  #clear() {
    for (const t of this.timers) { clearInterval(t); clearTimeout(t); }
    this.timers = [];
  }

  send() { /* the stream is one-way; nothing to say back */ }

  close() {
    this.readyState = 3;
    this.#clear();
    this.onclose?.({ code: 1000, reason: 'client close', wasClean: true });
  }
}

export function mockFactory({ flaky = false, seed = false } = {}) {
  return (url) => new MockSocket(url, { flaky, seed });
}
