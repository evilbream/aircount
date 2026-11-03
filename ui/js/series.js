// Pure data layer: raw point storage, bucketing, palette. No DOM, no network.
//
// Points come from two places — GET /history (ClickHouse) and the live socket —
// and both carry raw CSI windows, no pre-bucketed grid. The browser keeps every
// point and derives the grid for whatever window is selected. Rebuilding from
// raw on each update is a few tens of thousands of iterations at these rates,
// and it removes a whole class of bugs: no merge seam between history and live,
// no "late point in a closed bucket", and out-of-order arrivals simply land in
// the right bucket. The two sources overlap by design, so points are keyed by
// (sensor_id, timestamp): both carry the server's observed_at in ms.

export const TARGET_POINTS = 360;
export const NATIVE_WINDOW_MS = 5000; // one CSI window per sensor per 5s

// domain.presenceMotionThreshold. The stream carries no presence.rf verdict,
// so the chart applies the same threshold client-side and says so.
export const MOTION_THRESHOLD = 0.06;

// The kitchen and bedroom boards were flashed with each other's sensor_id. The
// stream and ClickHouse keep the ids as sent; only what the page shows is
// swapped. The id stays the key everywhere else: colours, ?sensors=, /history.
const SENSOR_LABELS = { bedroom: 'kitchen', kitchen: 'bedroom' };

export const sensorLabel = (id) => SENSOR_LABELS[id] ?? id;

export const RANGES = [
  { id: '5m',  label: '5 min',   ms: 5 * 60_000 },
  { id: '10m', label: '10 min',  ms: 10 * 60_000 },
  { id: '30m', label: '30 min',  ms: 30 * 60_000 },
  { id: '1h',  label: '1 hour',  ms: 60 * 60_000 },
  { id: '6h',  label: '6 hours', ms: 6 * 60 * 60_000 },
  { id: '24h', label: '24 hours', ms: 24 * 60 * 60_000 },
];

export const MAX_AGE_MS = RANGES[RANGES.length - 1].ms;

export function rangeById(id) {
  return RANGES.find((r) => r.id === id) ?? RANGES[3];
}

/** bucketMs = max(5s, ceilTo5s(rangeMs / TARGET_POINTS)). */
export function bucketMsFor(rangeMs, target = TARGET_POINTS) {
  const raw = Math.ceil(rangeMs / target);
  return Math.max(NATIVE_WINDOW_MS, Math.ceil(raw / NATIVE_WINDOW_MS) * NATIVE_WINDOW_MS);
}

export function bucketStart(tMs, bucketMs) {
  return Math.floor(tMs / bucketMs) * bucketMs;
}

export function fmtScore(v) {
  if (v == null) return '—';
  if (v >= 10) return v.toFixed(1);
  if (v >= 1) return v.toFixed(2);
  return v.toFixed(4);
}

export function fmtDur(ms) {
  const s = Math.max(0, Math.round(ms / 1000));
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m`;
  return `${Math.floor(m / 60)}h ${m % 60}m`;
}

// ---------------------------------------------------------------- palette

// Contrast-first on a dark ground, and no adjacent red/green pair so the chart
// stays readable with deuteranopia.
export const PALETTE = [
  '#4ea1ff', '#ffb454', '#c792ea', '#5ad1c3',
  '#f78c6c', '#ffd866', '#89ddff', '#b0bec5',
];

function fnv1a(str) {
  let h = 0x811c9dc5;
  for (let i = 0; i < str.length; i++) {
    h ^= str.charCodeAt(i);
    h = Math.imul(h, 0x01000193) >>> 0;
  }
  return h >>> 0;
}

/**
 * Stable colour per sensor_id: hashed, not arrival-ordered, so a sensor keeps
 * its colour across reloads and no matter which order sensors first appear in.
 */
export class Palette {
  constructor(colors = PALETTE) {
    this.colors = colors;
    this.byId = new Map();
    this.used = new Set();
  }
  colorFor(sensorId) {
    const cached = this.byId.get(sensorId);
    if (cached) return cached;
    const start = fnv1a(sensorId) % this.colors.length;
    let idx = start;
    for (let i = 0; i < this.colors.length; i++) {
      const cand = (start + i) % this.colors.length;
      if (!this.used.has(cand)) { idx = cand; break; }
    }
    this.used.add(idx);
    const color = this.colors[idx];
    this.byId.set(sensorId, color);
    return color;
  }
}

// ---------------------------------------------------------------- view

/**
 * One rendered window: the shared x grid plus a column per sensor, in uPlot's
 * native shape. Produced by RawStore.build; the chart only ever sees this.
 */
class View {
  constructor({ xs, order, cols, motion, bucketMs, collectingSinceMs, truncated, windowMs, partial }) {
    this.xs = xs;
    this.order = order;
    this.cols = cols;
    this.motion = motion;
    this.bucketMs = bucketMs;
    this.collectingSinceMs = collectingSinceMs;
    this.truncated = truncated;
    this.windowMs = windowMs;
    // partial: the selected window is wider than everything collected so far,
    // so the x axis was fitted to the data instead.
    this.partial = partial;
  }

  /**
   * uPlot data: [xs, ...one column per sensor]. Timestamps go to seconds
   * because uPlot's time scale works in unix seconds. On a log axis zeros are
   * clamped to a floor — a log scale cannot plot 0, and dropping the point
   * would misread as "sensor offline".
   */
  toUPlotData({ log = false, logFloor = 1e-4 } = {}) {
    const xs = this.xs.map((t) => t / 1000);
    const cols = this.order.map((id) => {
      const v = this.cols.get(id).v;
      return log ? v.map((x) => (x == null ? null : x > 0 ? x : logFloor)) : v;
    });
    return [xs, ...cols];
  }

  /** Indices that actually hold a value — used to mark real samples. */
  presentIndices(sensorId) {
    const col = this.cols.get(sensorId);
    if (!col) return [];
    const out = [];
    for (let i = 0; i < col.v.length; i++) if (col.v[i] != null) out.push(i);
    return out;
  }

  /**
   * Buckets holding far fewer samples than they could — a spike backed by one
   * window out of a possible 48 deserves a marker, not a confident line.
   */
  sparseIndices(sensorId) {
    const col = this.cols.get(sensorId);
    if (!col) return [];
    const expected = Math.max(1, Math.round(this.bucketMs / NATIVE_WINDOW_MS));
    if (expected < 2) return []; // native resolution: nothing is sparse
    const out = [];
    for (let i = 0; i < col.v.length; i++) {
      if (col.v[i] != null && col.n[i] * 2 < expected) out.push(i);
    }
    return out;
  }
}

// ---------------------------------------------------------------- raw store

function minOf(a, b) {
  if (a == null) return b;
  if (b == null) return a;
  return Math.min(a, b);
}

/** Sorts a column's parallel t/v arrays by t, in place. */
function sortCol(col) {
  let sorted = true;
  for (let i = 1; i < col.t.length && sorted; i++) sorted = col.t[i - 1] <= col.t[i];
  if (sorted) return;
  const idx = col.t.map((_, i) => i).sort((a, b) => col.t[a] - col.t[b]);
  const t = idx.map((i) => col.t[i]);
  const v = idx.map((i) => col.v[i]);
  col.t = t;
  col.v = v;
}

/**
 * Every point received, per sensor. The live store is capped at the longest
 * selectable window by trim(); a store for a past window is never trimmed.
 */
export class RawStore {
  /** @param {number} sinceMs  what is known from the start: now for the live
   *   store, the window's end for a past one (nothing of it is loaded yet). */
  constructor({ maxAgeMs = MAX_AGE_MS, sinceMs = Date.now() } = {}) {
    this.maxAgeMs = maxAgeMs;
    this.raw = new Map();       // sensorId -> { t: number[], v: number[], seen: Set<number> }
    // Until history arrives, nothing before sinceMs is known. The UI states
    // this rather than drawing an empty axis that reads as "nobody moved"; a
    // history load moves it back to where the server's answer starts.
    this.collectingSinceMs = sinceMs;
    // Start of the span GET /history has answered for, null until it has. From
    // here on an empty stretch is a real "no data", not "not loaded".
    this.coveredFromMs = null;
    // Tracked incrementally rather than read off col.t[0]: everything else here
    // tolerates out-of-order arrivals, and this should too.
    this.earliestMs = null;
  }

  #col(sensorId) {
    let col = this.raw.get(sensorId);
    if (!col) {
      col = { t: [], v: [], seen: new Set() };
      this.raw.set(sensorId, col);
    }
    return col;
  }

  /** @returns {boolean} false when this exact point is already stored */
  add(sensorId, tMs, score) {
    const col = this.#col(sensorId);
    if (col.seen.has(tMs)) return false;
    col.seen.add(tMs);
    col.t.push(tMs);
    col.v.push(score);
    if (this.earliestMs == null || tMs < this.earliestMs) this.earliestMs = tMs;
    if (tMs < this.collectingSinceMs) this.collectingSinceMs = tMs;
    return true;
  }

  /**
   * Merges a GET /history answer. History lands behind live points that are
   * already stored, so touched columns are re-sorted: trim() relies on order.
   *
   * @param {{sensorId: string, t: number, v: number}[]} points
   * @param {number|null} coversFromMs  start of the queried span when it extends
   *   the loaded window; null for a refetch of a recent stretch, which says
   *   nothing about what lies before it.
   * @returns {number} how many points were new
   */
  addHistory(points, coversFromMs = null) {
    const touched = new Set();
    let added = 0;
    for (const p of points) {
      if (this.add(p.sensorId, p.t, p.v)) { added++; touched.add(p.sensorId); }
    }
    for (const id of touched) sortCol(this.raw.get(id));
    if (coversFromMs != null) {
      this.coveredFromMs = this.coveredFromMs == null ? coversFromMs : Math.min(this.coveredFromMs, coversFromMs);
      if (coversFromMs < this.collectingSinceMs) this.collectingSinceMs = coversFromMs;
    }
    return added;
  }

  sensors() { return [...this.raw.keys()].sort(); }

  /** Drops points older than maxAge so memory stays bounded over days. */
  trim(nowMs = Date.now()) {
    const cutoff = nowMs - this.maxAgeMs;
    for (const col of this.raw.values()) {
      let drop = 0;
      while (drop < col.t.length && col.t[drop] < cutoff) drop++;
      if (drop > 0) {
        for (let i = 0; i < drop; i++) col.seen.delete(col.t[i]);
        col.t.splice(0, drop);
        col.v.splice(0, drop);
      }
    }
    this.#recomputeEarliest();
  }

  #recomputeEarliest() {
    let min = null;
    for (const col of this.raw.values()) {
      for (const t of col.t) if (min == null || t < min) min = t;
    }
    this.earliestMs = min;
  }

  /**
   * Derives the grid for one window. Buckets are epoch-aligned and aggregate
   * with max: the feature is spotting spikes, and a mean over a 4-minute bucket
   * buries a single 19.7 sample among 47 quiet ones.
   */
  build({ rangeMs, nowMs = Date.now(), threshold = MOTION_THRESHOLD }) {
    const bucketMs = bucketMsFor(rangeMs);
    const to = bucketStart(nowMs, bucketMs);
    const windowFrom = bucketStart(nowMs - rangeMs, bucketMs);

    // Until history covers the window (still loading, or the request failed)
    // its first minutes are empty by construction. Showing them anyway
    // squeezes the data into a sliver at the right edge and makes the chart
    // unreadable, so the axis starts at the first thing we actually know about:
    // the first point, or the start of the span the server answered for.
    const known = minOf(this.earliestMs, this.coveredFromMs);
    const from = known == null
      ? windowFrom
      : Math.max(windowFrom, bucketStart(known, bucketMs) - bucketMs);
    const partial = from > windowFrom;
    const count = Math.round((to - from) / bucketMs) + 1;

    const xs = new Array(count);
    for (let i = 0; i < count; i++) xs[i] = from + i * bucketMs;

    const order = this.sensors();
    const cols = new Map();
    const motion = new Map();

    for (const id of order) {
      const v = new Array(count).fill(null);
      const n = new Array(count).fill(0);
      const src = this.raw.get(id);

      for (let i = 0; i < src.t.length; i++) {
        const t = src.t[i];
        if (t < from) continue;
        const idx = Math.round((bucketStart(t, bucketMs) - from) / bucketMs);
        if (idx < 0 || idx >= count) continue;
        const x = src.v[i];
        v[idx] = v[idx] == null ? x : Math.max(v[idx], x);
        n[idx]++;
      }
      cols.set(id, { v, n });

      // No presence.rf on this stream, so "movement" is the same threshold the
      // detector uses, applied here. Labelled as such in the UI.
      const spans = [];
      let open = null;
      for (let i = 0; i < count; i++) {
        const hot = v[i] != null && v[i] >= threshold;
        if (hot && open == null) open = xs[i];
        if (!hot && open != null) { spans.push({ from: open, to: xs[i] }); open = null; }
      }
      if (open != null) spans.push({ from: open, to: to + bucketMs });
      motion.set(id, spans);
    }

    return new View({
      xs, order, cols, motion, bucketMs,
      collectingSinceMs: this.collectingSinceMs,
      truncated: from < this.collectingSinceMs,
      windowMs: rangeMs,
      partial,
    });
  }
}
