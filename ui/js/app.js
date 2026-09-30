// Entry point: wires the socket, the raw store and the chart to the controls.
//
// The server pushes one flat CSI window per message and nothing else:
//
//   {"sensor_id":"living-room","timestamp":1756713600000,"motion_score":0.045}
//
// All sensors share one connection; splitting into a series per sensor_id, the
// windowing and the bucketing all happen here.

import { RawStore, Palette, RANGES, rangeById, NATIVE_WINDOW_MS, MOTION_THRESHOLD } from './series.js';
import { Chart } from './chart.js';
import { WSClient } from './ws.js';
import { mockFactory } from './mock.js';
import { Journal } from './journal.js';

// No point for 3 windows running means the sensor or the gateway is down. That
// is a different failure from "the socket dropped", and a dashboard that
// conflates the two is a dashboard nobody trusts.
const STALE_AFTER_MS = 3 * NATIVE_WINDOW_MS;
const TRIM_EVERY_MS = 60_000;

const params = new URLSearchParams(location.search);
const useMock = params.get('mock') === '1';
const flaky = params.get('flaky') === '1';
const seed = params.get('seed') === '1';
// Deep-linkable view: ?range=6h&scale=linear survives a reload.
const initialRange = RANGES.some((r) => r.id === params.get('range')) ? params.get('range') : '1h';
const initialLog = params.get('scale') === 'log';
const initialJournal = params.get('journal') === '1';
const initialBars = params.get('shape') === 'bars';
// The UI deploys as a static folder that may or may not sit on the API's
// origin, so the endpoint is resolved at runtime, never baked in: ?ws= for a
// single tab, config.js for a standalone deploy, same origin otherwise.
const wsURL = params.get('ws')
  || globalThis.AIRCOUNT_CONFIG?.wsURL
  || `${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/ws`;

const $ = (id) => document.getElementById(id);
const els = {
  chart: $('chart'), ribbons: $('ribbons'), legend: $('legend'),
  ranges: $('ranges'), log: $('btn-log'), live: $('live-pill'),
  conn: $('conn'), connDot: $('conn-dot'), collecting: $('collecting'), mode: $('mode-badge'),
  hint: $('window-hint'), shape: $('btn-shape'), journal: $('journal'), journalBtn: $('btn-journal'), journalRows: $('journal-rows'),
  journalStats: $('journal-stats'), journalPause: $('journal-pause'), journalClear: $('journal-clear'),
};

const state = {
  range: initialRange,
  following: true,
  log: initialLog,
  bars: initialBars,
  wsState: 'connecting',
  retryInMs: null,
  lastPointAt: null,
};

const palette = new Palette();
const store = new RawStore();
const chart = new Chart({
  el: els.chart,
  ribbonEl: els.ribbons,
  legendEl: els.legend,
  palette,
  log: state.log,
  bars: initialBars,
  onFollowLost: (following) => {
    if (state.following === following) return;
    state.following = following;
    renderLivePill();
  },
});
chart.setThreshold(MOTION_THRESHOLD);

const journal = new Journal({
  rootEl: els.journal,
  rowsEl: els.journalRows,
  statsEl: els.journalStats,
  palette,
  threshold: MOTION_THRESHOLD,
});

// ---------------------------------------------------------------- rendering

let pending = false;
function scheduleRender() {
  if (pending) return;
  pending = true;
  requestAnimationFrame(() => {
    pending = false;
    // Rebuilt from raw every frame rather than merged incrementally: at these
    // rates it costs nothing and it means a range switch, a late point and a
    // brand-new sensor all take the exact same path.
    const view = store.build({ rangeMs: rangeById(state.range).ms, threshold: MOTION_THRESHOLD });
    chart.render(view, { follow: state.following });
    chart.updateSeen(store.lastSeen);
    renderHint(view);
    renderCollecting();
  });
}

function renderLivePill() { els.live.hidden = state.following; }

// The axis is fitted to the data while the window is not yet full, so say so —
// otherwise "5 min" over 39 seconds of data is a quietly wrong label.
function renderHint(view) {
  if (!view.partial || !view.xs.length) { els.hint.textContent = ''; return; }
  const span = view.xs[view.xs.length - 1] - view.xs[0] + view.bucketMs;
  els.hint.textContent = `showing ${fmtDur(span)} — the window is still filling`;
}

function fmtDur(ms) {
  const s = Math.max(0, Math.round(ms / 1000));
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m`;
  return `${Math.floor(m / 60)}h ${m % 60}m`;
}

const fmtClock = (ms) =>
  new Date(ms).toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit', hour12: false });

function renderConn() {
  const stale = state.wsState === 'live' && state.lastPointAt != null
    && Date.now() - state.lastPointAt > STALE_AFTER_MS;

  let cls = 'connecting';
  let text = 'connecting…';

  if (state.wsState === 'reconnecting') {
    cls = 'reconnecting';
    text = state.retryInMs == null
      ? 'reconnecting once the tab is visible again'
      : `reconnecting in ${Math.ceil(state.retryInMs / 1000)}s`;
  } else if (stale) {
    cls = 'stale';
    text = `no data for ${fmtDur(Date.now() - state.lastPointAt)} — the sensor or the gateway is silent`;
  } else if (state.wsState === 'live') {
    cls = 'live';
    text = `live · ${store.sensors().length} sensor(s)`;
  }

  els.connDot.className = `dot ${cls}`;
  els.conn.textContent = text;
}

function renderCollecting() {
  const since = store.collectingSinceMs;
  els.collecting.textContent =
    `collecting since ${fmtClock(since)} · ${fmtDur(Date.now() - since)} of data`;
  els.collecting.classList.add('warn');
  els.collecting.title =
    'The stream is live: the server only sends new CSI windows, there is no history '
    + 'from before the connection. Everything on screen was collected by this tab and '
    + 'is gone on reload.';
}

// ---------------------------------------------------------------- controls

function buildRangeButtons() {
  els.ranges.innerHTML = '';
  for (const r of RANGES) {
    const b = document.createElement('button');
    b.type = 'button';
    b.className = 'range-btn';
    b.dataset.range = r.id;
    b.textContent = r.label;
    if (r.ms > 60 * 60_000) {
      b.title = 'fills in while the tab keeps running: the server only sends the live stream';
    }
    b.addEventListener('click', () => selectRange(r.id));
    els.ranges.appendChild(b);
  }
  markActiveRange();
}

function markActiveRange() {
  for (const b of els.ranges.querySelectorAll('.range-btn')) {
    b.classList.toggle('active', b.dataset.range === state.range);
  }
}

function syncURL() {
  const q = new URLSearchParams(location.search);
  q.set('range', state.range);
  if (state.log) q.set('scale', 'log'); else q.delete('scale');
  if (state.bars) q.set('shape', 'bars'); else q.delete('shape');
  history.replaceState(null, '', `${location.pathname}?${q}`);
}

// Range switching is entirely local — the window is just a different view over
// the points this tab already holds, so there is nothing to ask the server for.
function selectRange(id) {
  state.range = id;
  state.following = true;
  renderLivePill();
  markActiveRange();
  syncURL();
  scheduleRender();
}

els.log.addEventListener('click', () => {
  state.log = !state.log;
  els.log.classList.toggle('active', state.log);
  els.log.textContent = state.log ? 'log' : 'linear';
  chart.setLogScale(state.log);
  syncURL();
  scheduleRender();
});

function setJournal(open) {
  journal.setOpen(open);
  els.journalBtn.classList.toggle('active', open);
  const q = new URLSearchParams(location.search);
  if (open) q.set('journal', '1'); else q.delete('journal');
  history.replaceState(null, '', `${location.pathname}?${q}`);
}

els.journalBtn.addEventListener('click', () => setJournal(els.journal.hidden));

els.journalPause.addEventListener('click', () => {
  const paused = !journal.paused;
  journal.setPaused(paused);
  els.journalPause.classList.toggle('active', paused);
  els.journalPause.textContent = paused ? 'resume' : 'pause';
});

els.journalClear.addEventListener('click', () => journal.clear());

els.shape.addEventListener('click', () => {
  state.bars = !state.bars;
  els.shape.classList.toggle('active', state.bars);
  els.shape.textContent = state.bars ? 'bars' : 'line';
  chart.setBars(state.bars);
  syncURL();
  scheduleRender();
});

els.live.addEventListener('click', () => {
  state.following = true;
  renderLivePill();
  scheduleRender();
});

// ---------------------------------------------------------------- frames

// Returns null when the frame is usable, otherwise why it is not. Kept
// separate from onFrame so the journal can report the exact reason instead of
// a message silently vanishing.
function rejectReason(frame) {
  if (frame === null || typeof frame !== 'object') return 'not an object';
  if (typeof frame.sensor_id !== 'string' || !frame.sensor_id) return 'no sensor_id';
  if (typeof frame.motion_score !== 'number' || !Number.isFinite(frame.motion_score)) {
    return 'no motion_score';
  }
  return null;
}

function onFrame(frame) {
  // One shape only. Anything else is logged and ignored rather than fatal, so a
  // server that starts sending richer messages cannot break this page.
  const reason = rejectReason(frame);
  journal.push(frame, reason);
  if (reason) return;

  const t = Number.isFinite(frame.timestamp) ? frame.timestamp : Date.now();
  store.add(frame.sensor_id, t, frame.motion_score);
  state.lastPointAt = Date.now();
  scheduleRender();
}

// ---------------------------------------------------------------- boot

const ws = new WSClient({
  url: wsURL,
  factory: useMock ? mockFactory({ flaky, seed }) : undefined,
  onFrame,
  onStatus: ({ state: s, retryInMs }) => {
    state.wsState = s;
    state.retryInMs = retryInMs ?? null;
    renderConn();
  },
});

if (useMock) {
  els.mode.hidden = false;
  els.mode.textContent = flaky ? 'MOCK · flaky' : seed ? 'MOCK · seed' : 'MOCK';
  els.mode.title = 'Synthetic stream in the same format, no backend (?mock=1)';
}

buildRangeButtons();
renderLivePill();
renderConn();
renderCollecting();
els.log.classList.toggle('active', state.log);
els.log.textContent = state.log ? 'log' : 'linear';
els.shape.classList.toggle('active', state.bars);
els.shape.textContent = state.bars ? 'bars' : 'line';

setJournal(initialJournal);
journal.renderStats();

setInterval(() => {
  renderConn();
  renderCollecting();
  chart.updateSeen(store.lastSeen);
  journal.renderStats();
}, 1000);
setInterval(() => store.trim(), TRIM_EVERY_MS);

// Draw the empty window before the first message lands. Otherwise the page sits
// as a blank rectangle for up to a full sensor window, which reads as broken
// rather than as waiting.
scheduleRender();
// Keep the axis moving while the stream is quiet, so "no data" stays visibly
// distinct from "frozen".
setInterval(() => { if (state.following) scheduleRender(); }, 5000);

ws.start();
