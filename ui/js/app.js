// Entry point: wires the socket, history, the raw store and the chart to the
// controls.
//
// The tab shows the sensors it is told to (?sensors=, else GET /api/list). What
// came before it opened is read from ClickHouse through GET /api/history, for
// the selected window only and extended as wider windows are picked:
//
//   GET /api/history?sensor=living-room,bedroom&from=<unix ms>&to=<unix ms>
//   {"sensor_ids":[…],"from":…,"to":…,
//    "history":[{"sensor_id":"living-room","timestamp":1756713600000,"motion_score":0.045},…]}
//
// What comes next arrives over the socket: one csi.<sensor_id> channel per
// sensor, one event per CSI window on each:
//
//   {"type":"event","channel":"csi.living-room","seq":42,"ts":1756713600000,
//    "payload":{"sensor_id":"living-room","timestamp":1756713600000,"motion_score":0.045}}
//
// Both carry the server's observed_at, so the store merges them by (sensor,
// timestamp). Splitting into a series per sensor_id, the windowing and the
// bucketing all happen here. The handshake itself lives in ws.js.

import {
  RawStore, Palette, RANGES, rangeById, bucketMsFor, NATIVE_WINDOW_MS, MOTION_THRESHOLD, fmtDur,
} from './series.js';
import { Chart } from './chart.js';
import { Rooms } from './rooms.js';
import { WSClient } from './ws.js';
import { mockFactory, mockHistory, MOCK_SENSORS } from './mock.js';
import { Journal } from './journal.js';

// No point for 3 windows running means the sensor or the gateway is down. That
// is a different failure from "the socket dropped", and a dashboard that
// conflates the two is a dashboard nobody trusts.
const STALE_AFTER_MS = 3 * NATIVE_WINDOW_MS;
const TRIM_EVERY_MS = 60_000;
const LIST_TIMEOUT_MS = 5000;
const LIST_RETRY_MS = 5000;
// 24h of three sensors is ~50k rows; give it time on a slow link.
const HISTORY_TIMEOUT_MS = 20_000;
const HISTORY_RETRY_MS = 5000;
// The ClickHouse writer flushes every 30s, and only once 50 rows have piled up,
// so the last minutes before any moment are not in /history yet: ~2 min with
// three sensors, ~5 with one. A stretch the socket did not deliver is asked for
// again on this schedule, until it must have landed.
const SETTLE_DELAYS_MS = [35_000, 2 * 60_000, 5 * 60_000];
// Slack around a socket outage: a window observed just before the drop may not
// have been delivered.
const GAP_PAD_MS = 2 * NATIVE_WINDOW_MS;

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
  || `${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/api/ws`;

// The hub has no wildcards and routes by exact channel name, so the tab has to
// know the sensors. GET /api/list is the source; ?sensors=living-room,bedroom pins
// them for one tab, and the mock has no API to ask.
const CSI_PREFIX = 'csi.';
const pinnedSensors = parseSensors(params.get('sensors')) ?? (useMock ? MOCK_SENSORS : null);
let channels = [];

function parseSensors(v) {
  if (v == null) return null;
  const list = (Array.isArray(v) ? v : String(v).split(','))
    .map((s) => String(s).trim())
    .filter(Boolean);
  return list.length ? [...new Set(list)] : null;
}

const $ = (id) => document.getElementById(id);
const els = {
  chart: $('chart'), empty: $('chart-empty'), ribbons: $('ribbons'), rooms: $('rooms'),
  ranges: $('ranges'), scale: $('scale'), live: $('live-pill'),
  conn: $('conn'), connDot: $('conn-dot'), collecting: $('collecting'), mode: $('mode-badge'),
  hint: $('window-hint'), shape: $('shape'), journal: $('journal'), journalBtn: $('btn-journal'), journalRows: $('journal-rows'),
  journalStats: $('journal-stats'), journalPause: $('journal-pause'), journalClear: $('journal-clear'),
};

const state = {
  range: initialRange,
  following: true,
  log: initialLog,
  bars: initialBars,
  wsState: pinnedSensors ? 'connecting' : 'listing',
  retryInMs: null,
  wsError: null,
  lastPointAt: null,
  histBusy: false,
  histError: null,
};

const palette = new Palette();
const store = new RawStore();
const chart = new Chart({
  el: els.chart,
  ribbonEl: els.ribbons,
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

const rooms = new Rooms({
  el: els.rooms,
  palette,
  threshold: MOTION_THRESHOLD,
  staleAfterMs: STALE_AFTER_MS,
  onToggle: (id, shown) => chart.setSensorShown(id, shown),
});

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
    rooms.render();
    els.empty.hidden = view.order.length > 0;
    renderHint(view);
    renderCollecting();
  });
}

function renderLivePill() { els.live.hidden = state.following; }

// The axis is fitted to the data while the window is not yet full, so say so —
// otherwise "5 min" over 39 seconds of data is a quietly wrong label.
function renderHint(view) {
  els.hint.textContent = view.partial && view.xs.length ? '· window still filling' : '';
}

const fmtClock = (ms) =>
  new Date(ms).toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit', hour12: false });

function renderConn() {
  const stale = state.wsState === 'live' && state.lastPointAt != null
    && Date.now() - state.lastPointAt > STALE_AFTER_MS;

  let cls = 'connecting';
  let text = 'connecting…';

  if (state.wsState === 'listing') {
    cls = state.wsError ? 'reconnecting' : 'connecting';
    text = state.wsError ? `sensor list failed, retrying: ${state.wsError}` : 'loading sensor list…';
  } else if (state.wsState === 'reconnecting') {
    cls = 'reconnecting';
    text = state.retryInMs == null
      ? 'reconnecting once the tab is visible again'
      : `reconnecting in ${Math.ceil(state.retryInMs / 1000)}s`;
  } else if (state.wsState === 'handshake') {
    text = 'connected, waiting for hello…';
  } else if (state.wsState === 'subscribing') {
    text = `subscribing to ${channels.length} channel(s)…`;
  } else if (state.wsState === 'rejected') {
    cls = 'reconnecting';
    text = `subscribe rejected: ${state.wsError}`;
  } else if (state.wsState === 'idle') {
    cls = 'stale';
    text = channels.length
      ? 'connected, subscribed to nothing'
      : 'the server lists no sensors — add ?sensors=living-room,bedroom to pick some';
  } else if (stale) {
    cls = 'stale';
    text = `no data for ${fmtDur(Date.now() - state.lastPointAt)} — the sensor or the gateway is silent`;
  } else if (state.wsState === 'live') {
    cls = 'live';
    text = 'live';
  }

  els.connDot.className = `dot ${cls}`;
  els.conn.textContent = text;
}

function renderCollecting() {
  const since = store.collectingSinceMs;
  let text = `${fmtDur(Date.now() - since)} of data, since ${fmtClock(since)}`;
  if (state.histBusy) text += ' · loading history…';
  else if (state.histError) text += ` · history failed, retrying: ${state.histError}`;
  els.collecting.textContent = text;
  els.collecting.title = store.coveredFromMs == null
    ? 'History has not loaded, so everything on screen was collected by this tab from '
      + 'the live stream and is gone on reload.'
    : 'The selected window is loaded from ClickHouse (/history); new windows arrive over '
      + 'the live stream. ClickHouse lags the stream by a few minutes, so anything the '
      + 'stream missed is filled in once it lands there.';
}

// ---------------------------------------------------------------- controls

// A control that names both options, so nobody has to work out whether a button
// labelled "linear" is the current state or what a tap would switch to.
function markSeg(group, value) {
  for (const b of group.querySelectorAll('button')) {
    const on = b.dataset.value === value;
    b.classList.toggle('active', on);
    b.setAttribute('aria-pressed', on);
  }
}

function onSeg(group, pick) {
  group.addEventListener('click', (e) => {
    const b = e.target.closest('button');
    if (b) pick(b.dataset.value);
  });
}

function buildRangeButtons() {
  els.ranges.innerHTML = '';
  for (const r of RANGES) {
    const b = document.createElement('button');
    b.type = 'button';
    b.dataset.value = r.id;
    // Six full labels do not fit across a phone.
    b.textContent = r.id;
    b.setAttribute('aria-label', r.label);
    els.ranges.appendChild(b);
  }
  markSeg(els.ranges, state.range);
}

function syncURL() {
  const q = new URLSearchParams(location.search);
  q.set('range', state.range);
  if (state.log) q.set('scale', 'log'); else q.delete('scale');
  if (state.bars) q.set('shape', 'bars'); else q.delete('shape');
  history.replaceState(null, '', `${location.pathname}?${q}`);
}

// The window is a view over the points this tab already holds; the server is
// asked only for the part of a wider window that is not loaded yet.
function selectRange(id) {
  state.range = id;
  state.following = true;
  renderLivePill();
  markSeg(els.ranges, id);
  syncURL();
  scheduleRender();
  ensureHistory();
}

onSeg(els.ranges, selectRange);

onSeg(els.scale, (value) => {
  state.log = value === 'log';
  markSeg(els.scale, value);
  chart.setLogScale(state.log);
  syncURL();
  scheduleRender();
});

function setJournal(open) {
  journal.setOpen(open);
  els.journalBtn.classList.toggle('active', open);
  els.journalBtn.setAttribute('aria-pressed', open);
  const q = new URLSearchParams(location.search);
  if (open) q.set('journal', '1'); else q.delete('journal');
  history.replaceState(null, '', `${location.pathname}?${q}`);
}

els.journalBtn.addEventListener('click', () => {
  setJournal(els.journal.hidden);
  // On a phone it opens below the fold, where the tap would seem to do nothing.
  if (!els.journal.hidden) els.journal.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
});

els.journalPause.addEventListener('click', () => {
  const paused = !journal.paused;
  journal.setPaused(paused);
  els.journalPause.classList.toggle('active', paused);
  els.journalPause.textContent = paused ? 'resume' : 'pause';
});

els.journalClear.addEventListener('click', () => journal.clear());

onSeg(els.shape, (value) => {
  state.bars = value === 'bars';
  markSeg(els.shape, value);
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

// Returns null when an event is usable, otherwise why it is not. Kept separate
// from onEvent so the journal can report the exact reason instead of a message
// silently vanishing.
function rejectReason(ev) {
  if (typeof ev.channel !== 'string' || !ev.channel.startsWith(CSI_PREFIX)) return 'not a csi channel';
  const p = ev.payload;
  if (p === null || typeof p !== 'object') return 'no payload';
  if (typeof p.sensor_id !== 'string' || !p.sensor_id) return 'no sensor_id';
  if (typeof p.motion_score !== 'number' || !Number.isFinite(p.motion_score)) {
    return 'no motion_score';
  }
  return null;
}

// Every message both ways goes to the journal; events are checked first so a
// malformed one shows up there with its reason instead of as a plain row.
function onMessage(msg, dir, reason = null) {
  if (dir === 'in' && msg?.type === 'event') reason = rejectReason(msg);
  journal.push(msg, reason, dir);
}

function onEvent(ev) {
  // Bad events are already in the journal with the reason; just skip them, so
  // a server that starts sending richer messages cannot break this page.
  if (rejectReason(ev)) return;

  // ts on the envelope is the observation time and is what the chart plots by.
  const t = Number.isFinite(ev.ts) ? ev.ts : Date.now();
  store.add(ev.payload.sensor_id, t, ev.payload.motion_score);
  rooms.add(ev.payload.sensor_id, ev.payload.motion_score);
  state.lastPointAt = Date.now();
  scheduleRender();
}

// ---------------------------------------------------------------- history

// list and history sit next to ws under the same prefix (/api behind nginx,
// none on a bare API host), so the one endpoint setting covers all three.
const apiURL = (path) => new URL(path, wsURL.replace(/^ws/, 'http'));

// Sensors the tab shows. /history is asked for exactly these: with no sensor
// parameter the server answers for all of them, and a sensor the socket is not
// subscribed to would show up as a line that never moves again.
let historySensors = [];

/** One [from, to) span, validated. Rows that do not parse are dropped, not fatal. */
async function fetchHistory(fromMs, toMs) {
  const from = Math.floor(fromMs);
  const to = Math.ceil(toMs);
  let body;
  if (useMock) {
    body = await mockHistory({ sensors: historySensors, from, to });
  } else {
    const url = apiURL('history');
    url.searchParams.set('sensor', historySensors.join(','));
    url.searchParams.set('from', String(from));
    url.searchParams.set('to', String(to));
    const res = await fetch(url, { signal: AbortSignal.timeout(HISTORY_TIMEOUT_MS) });
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    body = await res.json();
  }
  if (!Array.isArray(body?.history)) throw new Error('response has no history list');

  const wanted = new Set(historySensors);
  const points = [];
  let dropped = 0;
  for (const row of body.history) {
    const ok = row && wanted.has(row.sensor_id)
      && Number.isFinite(row.timestamp) && Number.isFinite(row.motion_score);
    if (ok) points.push({ sensorId: row.sensor_id, t: row.timestamp, v: row.motion_score });
    else dropped++;
  }
  if (dropped) console.warn(`/history: dropped ${dropped} malformed or unasked-for row(s)`);
  return points;
}

function applyHistory(points, coversFromMs) {
  store.addHistory(points, coversFromMs);
  const lastMotion = new Map();
  for (const p of points) {
    if (p.v < MOTION_THRESHOLD) continue;
    const prev = lastMotion.get(p.sensorId);
    if (prev == null || p.t > prev) lastMotion.set(p.sensorId, p.t);
  }
  for (const [id, t] of lastMotion) rooms.seedMotion(id, t);
  scheduleRender();
}

// Makes history reach back over the selected window. Only the part not loaded
// yet is asked for, so 1h → 24h fetches the 23h before it, and going back to
// 1h fetches nothing. One request at a time; a range picked meanwhile is
// picked up when it returns.
async function ensureHistory() {
  if (state.histBusy || !historySensors.length) return;
  clearTimeout(ensureHistory.retry);

  const rangeMs = rangeById(state.range).ms;
  // One bucket of slack, so the leftmost bucket is not built from a sliver.
  const want = Date.now() - rangeMs - bucketMsFor(rangeMs);
  const covered = store.coveredFromMs;
  if (covered != null && covered <= want) {
    state.histError = null;
    renderCollecting();
    return;
  }

  state.histBusy = true;
  state.histError = null;
  renderCollecting();
  try {
    applyHistory(await fetchHistory(want, covered ?? Date.now()), want);
  } catch (err) {
    state.histError = err?.message || String(err);
    ensureHistory.retry = setTimeout(ensureHistory, HISTORY_RETRY_MS);
    return;
  } finally {
    state.histBusy = false;
    renderCollecting();
  }
  ensureHistory();
}

// Asks for a stretch the socket did not deliver, on the SETTLE schedule, since
// ClickHouse may not have it yet. Each attempt is the previous one's retry, and
// points already held are skipped by the store, so repeats cost nothing.
function settle(fromMs, toMs, { now = false } = {}) {
  if (!historySensors.length) return;
  const delays = now ? [0, ...SETTLE_DELAYS_MS] : SETTLE_DELAYS_MS;
  for (const d of delays) {
    setTimeout(async () => {
      try {
        applyHistory(await fetchHistory(fromMs, toMs), null);
      } catch (err) {
        console.warn('/history refetch failed', err);
      }
    }, d);
  }
}

// Start of the stretch the socket has not delivered: at page load as far back
// as ClickHouse may lag (the first history load covers up to now, but its tail
// is not written yet), later the moment a live connection dropped.
let gapFromMs = Date.now() - SETTLE_DELAYS_MS.at(-1);
let everLive = false;

function trackGaps(prev, next) {
  if (next === 'live' && gapFromMs != null) {
    // After an outage nothing of it is held, so ask straight away too; on the
    // first connect the initial history load already did.
    settle(gapFromMs, Date.now(), { now: everLive });
    gapFromMs = null;
    everLive = true;
  } else if (prev === 'live' && next !== 'live') {
    gapFromMs = Date.now() - GAP_PAD_MS;
  }
}

// ---------------------------------------------------------------- boot

const ws = new WSClient({
  url: wsURL,
  factory: useMock ? mockFactory({ flaky, seed }) : undefined,
  onEvent,
  onMessage,
  onStatus: ({ state: s, retryInMs, error }) => {
    trackGaps(state.wsState, s);
    state.wsState = s;
    state.retryInMs = retryInMs ?? null;
    state.wsError = error ?? null;
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
markSeg(els.scale, state.log ? 'log' : 'linear');
markSeg(els.shape, state.bars ? 'bars' : 'line');

setJournal(initialJournal);
journal.renderStats();

setInterval(() => {
  renderConn();
  renderCollecting();
  rooms.render();
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

async function fetchSensors() {
  const res = await fetch(apiURL('list'), { signal: AbortSignal.timeout(LIST_TIMEOUT_MS) });
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  const list = await res.json();
  if (!Array.isArray(list)) throw new Error('response is not a list');
  return parseSensors(list) ?? [];
}

function startSocket(sensors) {
  channels = sensors.map((id) => CSI_PREFIX + id);
  historySensors = sensors;
  rooms.setSensors(sensors);
  ws.setChannels(channels);
  ws.start();
  ensureHistory();
}

// Nothing to subscribe to until the list arrives, so keep asking rather than
// connect to an empty set the tab would then never leave.
async function loadSensors() {
  let sensors;
  try {
    sensors = await fetchSensors();
  } catch (err) {
    state.wsError = err.message;
    renderConn();
    setTimeout(loadSensors, LIST_RETRY_MS);
    return;
  }
  startSocket(sensors);
}

if (pinnedSensors) startSocket(pinnedSensors);
else loadSensors();
