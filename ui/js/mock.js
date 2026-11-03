// Offline mode (?mock=1): stands in for the socket and speaks the same
// aircount.v1 protocol as simple-ws — hello on connect, ack on subscribe, pong
// on ping, and one event per subscribed csi.<sensor> channel every window. Lets
// the UI be worked on without the backend, and afterwards serves as the fixture
// when frontend and backend disagree.
//
// Any csi.<sensor_id> channel is accepted, so ?sensors= decides what it emits;
// without it the tab subscribes to MOCK_SENSORS, there being no /list to ask.
// GET /history is stood in for by mockHistory, built from the same scoreAt, so a
// window reads the same whether it came from history or from the stream.
//
//   ?mock=1          history for the selected window, plus one window per
//                    subscribed sensor every 5s
//   ?mock=1&seed=1   plus ~2h of points backdated over the socket itself
//   ?mock=1&flaky=1  plus a dropped connection at 45s, to watch the gap refill

import { NATIVE_WINDOW_MS } from './series.js';

const SEED_SPAN_MS = 2 * 60 * 60_000;
const HEARTBEAT_MS = 25_000;
const HISTORY_LATENCY_MS = 300;
const CSI_PREFIX = 'csi.';

// Timestamps sit on the 5s grid, so history and live agree on (sensor, ts) for
// the same window and the store's dedupe sees them as one point, as it does for
// the real server.
const slotOf = (tMs) => Math.floor(tMs / NATIVE_WINDOW_MS) * NATIVE_WINDOW_MS;

export const MOCK_SENSORS = ['living-room', 'bedroom'];

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
    this.channels = new Set();
    this.seqs = new Map(); // channel -> last seq, per channel like the hub
    this.connId = `mock-${Math.random().toString(16).slice(2, 10)}`;
    setTimeout(() => this.#open(), 150);
  }

  #reply(msg) {
    if (this.readyState !== 1 || !this.onmessage) return;
    this.onmessage({ data: JSON.stringify(msg) });
  }

  #emit(channel, tMs, score) {
    const seq = (this.seqs.get(channel) ?? 0) + 1;
    this.seqs.set(channel, seq);
    const sensorId = channel.slice(CSI_PREFIX.length);
    this.#reply({
      type: 'event', channel, seq, ts: tMs,
      payload: { sensor_id: sensorId, timestamp: tMs, motion_score: score },
    });
  }

  #open() {
    this.readyState = 1;
    this.onopen?.({});
    this.#reply({
      type: 'hello', protocol: 'aircount.v1', conn_id: this.connId,
      server_ts: Date.now(), heartbeat_ms: HEARTBEAT_MS,
      limits: { max_channels: 10, max_msg_bytes: 1024 },
    });

    this.timers.push(setInterval(() => {
      const slot = slotOf(Date.now());
      for (const ch of this.channels) {
        const v = scoreAt(ch.slice(CSI_PREFIX.length), slot);
        if (v != null) this.#emit(ch, slot, v);
      }
    }, NATIVE_WINDOW_MS));

    // Drop once, mid-session, so the reconnect path gets exercised for real.
    if (this.flaky) this.timers.push(setTimeout(() => this.#drop(), 45_000));
  }

  #backfill(channel) {
    const now = Date.now();
    for (let t = slotOf(now - SEED_SPAN_MS); t <= now; t += NATIVE_WINDOW_MS) {
      const v = scoreAt(channel.slice(CSI_PREFIX.length), t);
      if (v != null) this.#emit(channel, t, v);
    }
  }

  #handle(msg) {
    const sorted = () => [...this.channels].sort();
    switch (msg.op) {
      case 'subscribe': {
        const bad = (msg.channels ?? []).filter((c) => !c.startsWith(CSI_PREFIX) || c === CSI_PREFIX);
        if (bad.length) {
          this.#reply({ type: 'error', id: msg.id, code: 400, message: `unknown channel ${JSON.stringify(bad[0])}` });
          return;
        }
        const added = msg.channels.filter((c) => !this.channels.has(c));
        for (const c of added) this.channels.add(c);
        this.#reply({ type: 'ack', id: msg.id, op: 'subscribe', subscribed: sorted() });
        if (this.seed) for (const c of added) this.#backfill(c);
        return;
      }
      case 'unsubscribe':
        for (const c of msg.channels ?? []) this.channels.delete(c);
        this.#reply({ type: 'ack', id: msg.id, op: 'unsubscribe', subscribed: sorted() });
        return;
      case 'ping':
        this.#reply({ type: 'pong', id: msg.id, server_ts: Date.now() });
        return;
      default:
        this.#reply({ type: 'error', id: msg.id, code: 400, message: `unknown op ${JSON.stringify(msg.op)}` });
    }
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

  send(data) {
    if (this.readyState !== 1) return;
    let msg;
    try { msg = JSON.parse(data); } catch { return; }
    // Async, like a real round trip: the reply must not land inside send().
    setTimeout(() => this.#handle(msg), 20);
  }

  close() {
    if (this.readyState === 3) return;
    this.readyState = 3;
    this.#clear();
    this.onclose?.({ code: 1000, reason: 'client close', wasClean: true });
  }
}

export function mockFactory({ flaky = false, seed = false } = {}) {
  return (url) => new MockSocket(url, { flaky, seed });
}

/**
 * Stand-in for GET /history: the same body the server sends, for windows in
 * [from, to) of the given sensors, ordered by time.
 */
export async function mockHistory({ sensors, from, to }) {
  await new Promise((r) => setTimeout(r, HISTORY_LATENCY_MS));
  const history = [];
  for (let t = slotOf(from); t < to; t += NATIVE_WINDOW_MS) {
    if (t < from) continue;
    for (const id of sensors) {
      const v = scoreAt(id, t);
      if (v != null) history.push({ sensor_id: id, timestamp: t, motion_score: v });
    }
  }
  return { sensor_ids: sensors, from, to, history };
}
