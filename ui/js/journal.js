// On-screen log of what actually arrived on the socket.
//
// It records rejected frames too, with the reason: a journal that only shows
// what parsed cleanly is silent exactly when the protocol has drifted, which is
// the one moment you need it.

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
   */
  push(frame, reason = null) {
    const at = Date.now();
    this.received++;
    if (reason) this.rejected++;
    this.stamps.push(at);

    // Paused freezes the rows, not the counters — you still want to see whether
    // anything is arriving at all.
    if (this.paused) return;

    this.pendingRows.push({ at, frame, reason });
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

  #buildRow({ at, frame, reason }) {
    const row = document.createElement('div');
    row.className = 'jrow' + (reason ? ' bad' : '');

    const time = document.createElement('span');
    time.className = 'jtime';
    time.textContent = clock(at);
    row.appendChild(time);

    const id = typeof frame?.sensor_id === 'string' ? frame.sensor_id : '—';
    const dot = document.createElement('span');
    dot.className = 'jdot';
    if (!reason) dot.style.background = this.palette.colorFor(id);
    row.appendChild(dot);

    const name = document.createElement('span');
    name.className = 'jname';
    name.textContent = id;
    row.appendChild(name);

    const score = document.createElement('span');
    const v = frame?.motion_score;
    score.className = 'jscore' + (!reason && typeof v === 'number' && v >= this.threshold ? ' hot' : '');
    score.textContent = fmtScore(v);
    row.appendChild(score);

    const note = document.createElement('span');
    note.className = 'jnote';
    if (reason) {
      note.textContent = reason;
      // The raw payload is what settles a protocol argument, so keep it to hand.
      row.title = JSON.stringify(frame);
    } else {
      // Lag between the gateway's stamp and arrival here.
      const lag = Number.isFinite(frame.timestamp) ? at - frame.timestamp : null;
      note.textContent = lag == null ? 'no timestamp' : `+${lag} ms`;
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
