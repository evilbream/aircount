// On-screen log of what actually went over the socket, both ways.
//
// It records rejected frames too, with the reason: a journal that only shows
// what parsed cleanly is silent exactly when the protocol has drifted, which is
// the one moment you need it. Service messages (hello, ack, ping/pong, error)
// are shown too, dimmed, because "subscribed to nothing" and "nothing arrives"
// look identical on the chart.

const MAX_ROWS = 300;
const RATE_WINDOW_MS = 5000;

const pad = (n, w = 2) => String(n).padStart(w, '0');

function clock(ms) {
  const d = new Date(ms);
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}.${pad(d.getMilliseconds(), 3)}`;
}

function fmtScore(v) {
  if (typeof v !== 'number' || !Number.isFinite(v)) return '—';
  if (v >= 10) return v.toFixed(2);
  if (v >= 1) return v.toFixed(3);
  return v.toFixed(4);
}

// One line on a service message; the full JSON is in the row's tooltip.
function summarize(m) {
  if (!m || typeof m !== 'object') return '';
  if (Array.isArray(m.channels)) return m.channels.join(', ');
  if (Array.isArray(m.subscribed)) return m.subscribed.join(', ') || 'nothing';
  if (m.type === 'hello') return `conn ${m.conn_id ?? '?'}`;
  if (m.type === 'error') return m.message ?? String(m.code ?? '');
  if (m.type === 'dropped') return `${m.channel}: ${m.count} lost`;
  if (m.type === 'bye') return m.reason ?? '';
  return m.id != null ? `id ${m.id}` : '';
}

export class Journal {
  constructor({ rootEl, rowsEl, statsEl, palette, threshold, max = MAX_ROWS }) {
    this.rootEl = rootEl;
    this.rowsEl = rowsEl;
    this.statsEl = statsEl;
    this.palette = palette;
    this.threshold = threshold;
    this.max = max;

    this.open = false;
    this.paused = false;
    this.received = 0;
    this.rejected = 0;
    this.stamps = [];      // arrival times, for the rate readout
    this.pendingRows = []; // flushed on the next frame
    this.flushQueued = false;
    this.stick = true;     // follow the tail unless the user scrolled away

    rowsEl.addEventListener('scroll', () => {
      const gap = rowsEl.scrollHeight - rowsEl.scrollTop - rowsEl.clientHeight;
      this.stick = gap < 24;
    });
  }

  setOpen(open) {
    this.open = open;
    this.rootEl.hidden = !open;
    if (open) { this.stick = true; this.#flush(true); }
  }

  setPaused(paused) { this.paused = paused; }

  clear() {
    this.pendingRows = [];
    this.rowsEl.replaceChildren();
    this.received = 0;
    this.rejected = 0;
    this.stamps = [];
    this.renderStats();
  }

  /**
   * @param {object} frame  the parsed message, whatever shape it had
   * @param {string|null} reason  null when accepted, otherwise why it was dropped
   * @param {'in'|'out'} dir  received from the server, or sent by this tab
   */
  push(frame, reason = null, dir = 'in') {
    const at = Date.now();
    if (dir === 'in') {
      this.received++;
      if (reason) this.rejected++;
      this.stamps.push(at);
    }

    // Paused freezes the rows, not the counters — you still want to see whether
    // anything is arriving at all.
    if (this.paused) return;

    this.pendingRows.push({ at, frame, reason, dir });
    if (this.pendingRows.length > this.max) {
      this.pendingRows.splice(0, this.pendingRows.length - this.max);
    }
    this.#queueFlush();
  }

  #queueFlush() {
    if (this.flushQueued) return;
    this.flushQueued = true;
    requestAnimationFrame(() => { this.flushQueued = false; this.#flush(); });
  }

  #flush(force = false) {
    if (!this.open && !force) {
      // Closed: keep only what would still be visible when reopened.
      if (this.pendingRows.length > this.max) {
        this.pendingRows.splice(0, this.pendingRows.length - this.max);
      }
      return;
    }
    if (!this.pendingRows.length) return;

    const frag = document.createDocumentFragment();
    for (const row of this.pendingRows) frag.appendChild(this.#buildRow(row));
    this.pendingRows = [];
    this.rowsEl.appendChild(frag);

    // Trim from the top so a long session cannot grow the DOM without bound.
    let excess = this.rowsEl.childElementCount - this.max;
    while (excess-- > 0 && this.rowsEl.firstElementChild) {
      this.rowsEl.removeChild(this.rowsEl.firstElementChild);
    }
    if (this.stick) this.rowsEl.scrollTop = this.rowsEl.scrollHeight;
  }

  #buildRow({ at, frame, reason, dir }) {
    const isEvent = frame?.type === 'event';
    const row = document.createElement('div');
    row.className = 'jrow' + (reason ? ' bad' : isEvent ? '' : ' svc');
    // The raw message is what settles a protocol argument, so keep it to hand.
    row.title = JSON.stringify(frame);

    const time = document.createElement('span');
    time.className = 'jtime';
    time.textContent = clock(at);
    row.appendChild(time);

    const payload = isEvent ? frame.payload : null;
    const id = typeof payload?.sensor_id === 'string' ? payload.sensor_id : null;

    const dot = document.createElement('span');
    dot.className = 'jdot';
    if (!reason && id) dot.style.background = this.palette.colorFor(id);
    row.appendChild(dot);

    const name = document.createElement('span');
    name.className = 'jname';
    name.textContent = isEvent ? (id ?? frame.channel ?? '—') : `${dir === 'out' ? '→' : '←'} ${frame?.type ?? frame?.op ?? '?'}`;
    row.appendChild(name);

    const score = document.createElement('span');
    const v = payload?.motion_score;
    score.className = 'jscore' + (!reason && typeof v === 'number' && v >= this.threshold ? ' hot' : '');
    score.textContent = isEvent ? fmtScore(v) : '';
    row.appendChild(score);

    const note = document.createElement('span');
    note.className = 'jnote';
    if (reason) {
      note.textContent = reason;
    } else if (isEvent) {
      // Lag between the observation stamp and arrival here.
      const lag = Number.isFinite(frame.ts) ? at - frame.ts : null;
      note.textContent = lag == null ? 'no ts' : `#${frame.seq} +${lag} ms`;
    } else {
      note.textContent = summarize(frame);
    }
    row.appendChild(note);
    return row;
  }

  renderStats() {
    const now = Date.now();
    while (this.stamps.length && this.stamps[0] < now - RATE_WINDOW_MS) this.stamps.shift();
    const rate = (this.stamps.length / (RATE_WINDOW_MS / 1000)).toFixed(1);
    this.statsEl.textContent =
      `received ${this.received} · rejected ${this.rejected} · ${rate}/s`;
    this.statsEl.classList.toggle('warn', this.rejected > 0);
  }
}
