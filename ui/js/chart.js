// uPlot wiring: series, log/linear axis, follow mode, and the three overlays
// that make the chart honest — the "no data loaded" band, the motion-detection
// ribbons, and the presence threshold line.

import uPlot from 'uplot';
import 'uplot/dist/uPlot.min.css';

import { Palette, NATIVE_WINDOW_MS, fmtScore } from './series.js';
import { bindTouch } from './touch.js';

const LOG_FLOOR = 1e-4;
const BAR_GAP_FRAC = 0.18;
const TIP_REACH = 2; // buckets to look around an empty one for the readout

function withAlpha(hex, a) {
  const n = parseInt(hex.slice(1), 16);
  return `rgba(${(n >> 16) & 255}, ${(n >> 8) & 255}, ${n & 255}, ${a})`;
}

/**
 * Bars whose height is the bucket's motion_score, one slot per sensor inside
 * each bucket so several sensors never paint over each other.
 *
 * uPlot ships a bars path builder, but it centres every series on the same x
 * and gives no way to offset them, so with more than one sensor the bars would
 * overlap. This is that builder plus the group offset.
 */
function groupedBars({ groupCount, groupIdx }) {
  return (u, sIdx, i0, i1) => {
    const xd = u.data[0];
    const yd = u.data[sIdx];
    const path = new Path2D();

    // Baseline is the bottom of the y scale: on a linear axis that is 0, so bar
    // height is literally proportional to the score.
    const yBase = u.valToPos(u.scales.y.min, 'y', true);

    const colPx = xd.length > 1
      ? Math.abs(u.valToPos(xd[1], 'x', true) - u.valToPos(xd[0], 'x', true))
      : 10;
    const groupW = Math.max(1, colPx * (1 - BAR_GAP_FRAC));
    const barW = Math.max(0.6, groupW / groupCount);

    for (let i = i0; i <= i1; i++) {
      const v = yd[i];
      if (v == null) continue;
      const y = u.valToPos(v, 'y', true);
      const h = yBase - y;
      if (!(h > 0)) continue;
      const left = u.valToPos(xd[i], 'x', true) - groupW / 2 + groupIdx * barW;
      path.rect(left, y, barW, h);
    }
    return { stroke: path, fill: path };
  };
}

const fmtTime = (unixS, seconds) =>
  new Date(unixS * 1000).toLocaleTimeString('en-GB', {
    hour: '2-digit', minute: '2-digit', second: seconds ? '2-digit' : undefined, hour12: false,
  });

// fmtScore pads to 4 decimals for the readouts; on an axis that is just noise.
const fmtTick = (v) => (v == null ? '' : String(+v.toPrecision(3)));

/** Buckets are regularly left empty (see spanGaps), and "—" there would be a lie. */
function nearestValue(col, i) {
  for (let d = 0; d <= TIP_REACH; d++) {
    if (col[i - d] != null) return col[i - d];
    if (col[i + d] != null) return col[i + d];
  }
  return null;
}

/** 8x8 diagonal hatch, built once, used to shade "there is no data here". */
function hatchPattern(ctx, color) {
  const c = document.createElement('canvas');
  c.width = c.height = 8;
  const g = c.getContext('2d');
  g.strokeStyle = color;
  g.lineWidth = 1;
  g.beginPath();
  g.moveTo(-2, 10); g.lineTo(10, -2);
  g.moveTo(-2, 18); g.lineTo(18, -2);
  g.stroke();
  return ctx.createPattern(c, 'repeat');
}

export class Chart {
  /**
   * @param {HTMLElement} el       chart container
   * @param {HTMLElement} ribbonEl strip under the chart for presence.rf spans
   */
  constructor({ el, ribbonEl, palette = new Palette(), log = true, bars = false, onFollowLost }) {
    this.el = el;
    this.ribbonEl = ribbonEl;
    this.palette = palette;
    this.onFollowLost = onFollowLost;

    this.u = null;
    this.sensors = [];
    this.hidden = new Set();
    this.store = null;
    this.log = log;
    this.bars = bars;
    this.threshold = null;
    this.collectingSinceMs = null;
    this.truncated = false;

    // Next to uPlot's root rather than inside it: that one goes on every rebuild.
    this.tip = document.createElement('div');
    this.tip.className = 'chart-tip';
    this.tip.hidden = true;
    el.appendChild(this.tip);

    bindTouch(el, {
      plot: () => this.u,
      onZoom: (full) => this.onFollowLost?.(full),
    });

    // Set while we drive the scales ourselves, so our own setData doesn't get
    // mistaken for the user panning and silently kill follow mode.
    this.applying = false;

    this._hatch = null;
    this._ro = new ResizeObserver(() => this.#resize());
    this._ro.observe(el);

    el.addEventListener('dblclick', () => {
      // uPlot's own dblclick resets the zoom and fires setScale, which would
      // clear follow mode. Re-arm it after that has settled.
      setTimeout(() => this.onFollowLost?.(true), 0);
    });
  }

  setThreshold(v) { this.threshold = v; this.u?.redraw(); }

  setLogScale(on) {
    if (this.log === on) return;
    this.log = on;
    this.#rebuild(); // scale distr is baked into the instance
  }

  setBars(on) {
    if (this.bars === on) return;
    this.bars = on;
    this.#rebuild(); // the path builder is baked in per series
  }

  setSensorShown(id, show) {
    if (show) this.hidden.delete(id); else this.hidden.add(id);
    const i = this.sensors.indexOf(id);
    if (i >= 0) this.u?.setSeries(i + 1, { show });
  }

  /** Rebuilds when the sensor set changes; otherwise just pushes data. */
  render(view, { follow }) {
    const store = this.store = view;
    this.collectingSinceMs = store.collectingSinceMs;
    this.truncated = store.truncated;

    const same =
      this.u &&
      this.sensors.length === store.order.length &&
      this.sensors.every((id, i) => id === store.order[i]);

    if (!same) {
      this.sensors = store.order.slice();
      this.#rebuild(follow);
      return;
    }
    this.#push(follow);
  }

  #push(follow) {
    if (!this.u) return;
    this.applying = true;
    this.u.setData(this.store.toUPlotData({ log: this.log, logFloor: LOG_FLOOR }), follow);
    this.applying = false;
    this.#renderTip(this.u);
    this.#drawRibbons();
  }

  #rebuild(follow = true) {
    const data = this.store
      ? this.store.toUPlotData({ log: this.log, logFloor: LOG_FLOOR })
      : [[]];

    const series = [
      { label: 'time' },
      ...this.sensors.map((id, i) => ({
        label: id,
        show: !this.hidden.has(id),
        stroke: this.palette.colorFor(id),
        fill: this.bars ? withAlpha(this.palette.colorFor(id), 0.55) : undefined,
        paths: this.bars
          ? groupedBars({ groupCount: this.sensors.length, groupIdx: i })
          : undefined,
        width: this.bars ? 1 : 1.6,
        // The sensor's window (~5s) and the native bucket (5s) beat against
        // each other, so a bucket is regularly left empty even while the sensor
        // is reporting steadily. Without spanning, the series shatters into
        // single-sample fragments — and a lone point draws nothing at all,
        // because a line segment needs two. Real absences stay visible through
        // the sample markers below and the journal.
        spanGaps: true,
        points: {
          show: !this.bars,
          size: 5,
          stroke: this.palette.colorFor(id),
          fill: '#11161c',
          // Returning null would mean "no filter" and uPlot would mark every
          // bucket; the empty array is what actually suppresses markers.
          //
          // At native resolution every real sample gets a marker, so now that
          // gaps are spanned you can still see where the data actually is.
          // On coarser buckets that would be noise, so only buckets built from
          // far fewer samples than they could hold are marked — a spike backed
          // by 1 window out of 48 is worth flagging.
          filter: (_u, sIdx) => {
            const id = this.sensors[sIdx - 1];
            if (!this.store || !id) return [];
            return this.store.bucketMs <= NATIVE_WINDOW_MS
              ? this.store.presentIndices(id)
              : this.store.sparseIndices(id);
          },
        },
      })),
    ];

    const opts = {
      width: this.el.clientWidth || 800,
      height: this.el.clientHeight || 380,
      // The right side has to fit half of a centred "22:41:50" tick label.
      padding: [12, 24, 0, 0],
      cursor: { drag: { x: true, y: false }, focus: { prox: 24 } },
      // The sensor cards and the tip do this job, and they also work on a phone.
      legend: { show: false },
      scales: {
        x: { time: true },
        y: this.log
          // fullMags=false keeps the axis on the data instead of snapping out
          // to whole decades, which otherwise flattens a 0.08..0.13 spread into
          // a sliver of a 0.05..1.0 axis.
          ? { distr: 3, log: 10, range: (_u, min, max) => uPlot.rangeLog(min, max, 10, false) }
          // Bars are read against a zero baseline; an auto-ranged floor would
          // make a 0.09 bar look taller than a 0.13 one on a different window.
          : this.bars
            // Bars are read against zero: an auto-ranged floor would make a
            // 0.09 bar look taller than a 0.13 one on a different window.
            ? { distr: 1, range: (_u, _min, max) => [0, (max ?? 1) * 1.08] }
            : { distr: 1 },
      },
      axes: [
        {
          stroke: '#7d8894',
          size: 30,
          gap: 4,
          space: 64,
          grid: { stroke: 'rgba(255,255,255,0.06)', width: 1 },
          ticks: { stroke: 'rgba(255,255,255,0.12)', width: 1, size: 4 },
          values: (_u, ticks, _ax, _space, incr) => ticks.map((t) => fmtTime(t, incr < 60)),
        },
        {
          stroke: '#7d8894',
          size: 46,
          gap: 4,
          grid: { stroke: 'rgba(255,255,255,0.06)', width: 1 },
          ticks: { stroke: 'rgba(255,255,255,0.12)', width: 1, size: 4 },
          values: (_u, vals) => vals.map(fmtTick),
        },
      ],
      series,
      hooks: {
        // Backgrounds go in drawClear so the lines render on top of them.
        drawClear: [(u) => { this.#drawNoDataBand(u); }],
        draw: [(u) => { this.#drawThreshold(u); this.#drawRibbons(); }],
        setCursor: [(u) => this.#renderTip(u)],
        setSelect: [(u) => {
          // Fires only on a real drag-zoom, unlike setScale which also fires on
          // resize and on our own setData.
          if (!this.applying && u.select.width > 0) this.onFollowLost?.(false);
        }],
        setSize: [() => this.#drawRibbons()],
      },
    };

    this.#buildTip();
    this.u?.destroy();
    this.applying = true;
    this.u = new uPlot(opts, data, this.el);
    this.applying = false;
    this.#drawRibbons();
  }

  #buildTip() {
    this.tip.hidden = true;
    this._tipTime = document.createElement('div');
    this._tipTime.className = 'tip-time';
    this._tipRows = this.sensors.map((id) => {
      const row = document.createElement('div');
      row.className = 'tip-row';
      const swatch = document.createElement('span');
      swatch.className = 'swatch';
      swatch.style.background = this.palette.colorFor(id);
      const name = document.createElement('span');
      name.className = 'tip-name';
      name.textContent = id;
      const val = document.createElement('span');
      val.className = 'tip-val';
      row.append(swatch, name, val);
      return { row, val };
    });
    this.tip.replaceChildren(this._tipTime, ...this._tipRows.map((r) => r.row));
  }

  /**
   * The readout under the cursor. It sits at the top of the plot, on the side
   * away from the cursor, so a finger is never on top of the numbers it asks for.
   */
  #renderTip(u) {
    const i = u.cursor.idx;
    if (i == null || !this.store) { this.tip.hidden = true; return; }

    this._tipTime.textContent = fmtTime(u.data[0][i], this.store.bucketMs < 60_000);
    this.sensors.forEach((id, n) => {
      const { row, val } = this._tipRows[n];
      row.hidden = this.hidden.has(id);
      const v = nearestValue(u.data[n + 1], i);
      val.textContent = fmtScore(v);
      val.classList.toggle('hot', v != null && this.threshold != null && v >= this.threshold);
    });

    const dpr = u.pxRatio ?? window.devicePixelRatio ?? 1;
    const left = u.bbox.left / dpr;
    const width = u.bbox.width / dpr;
    const onRight = u.cursor.left < width / 2;
    this.tip.style.left = onRight ? 'auto' : `${left + 6}px`;
    this.tip.style.right = onRight ? `${u.width - left - width + 6}px` : 'auto';
    this.tip.hidden = false;
  }

  /**
   * Everything left of collecting_since is not "no motion", it is "nothing
   * loaded": history still on its way or failed, and only what the socket
   * delivered is held. Without this band an empty axis reads as a quiet house.
   */
  #drawNoDataBand(u) {
    // With no sensors at all the empty-chart message says what is going on.
    if (!this.sensors.length || !this.truncated || this.collectingSinceMs == null) return;
    const { ctx, bbox } = u;
    const xEnd = u.valToPos(this.collectingSinceMs / 1000, 'x', true);
    const xStart = bbox.left;
    if (!(xEnd > xStart)) return;

    this._hatch ||= hatchPattern(ctx, 'rgba(255,255,255,0.07)');
    ctx.save();
    ctx.beginPath();
    ctx.rect(bbox.left, bbox.top, bbox.width, bbox.height);
    ctx.clip();
    ctx.fillStyle = 'rgba(255,255,255,0.025)';
    ctx.fillRect(xStart, bbox.top, xEnd - xStart, bbox.height);
    ctx.fillStyle = this._hatch;
    ctx.fillRect(xStart, bbox.top, xEnd - xStart, bbox.height);

    const dpr = u.pxRatio ?? window.devicePixelRatio ?? 1;
    ctx.fillStyle = 'rgba(210,220,230,0.55)';
    ctx.font = `${11 * dpr}px ui-monospace, SFMono-Regular, Menlo, monospace`;
    ctx.textAlign = 'center';
    ctx.textBaseline = 'middle';
    const label = 'no data loaded';
    if (xEnd - xStart > ctx.measureText(label).width + 16 * dpr) {
      ctx.fillText(label, (xStart + xEnd) / 2, bbox.top + bbox.height / 2);
    }
    ctx.restore();
  }

  #drawThreshold(u) {
    if (this.threshold == null) return;
    const y = this.threshold <= 0 && this.log ? null : u.valToPos(this.threshold, 'y', true);
    if (y == null || !Number.isFinite(y)) return;
    const { ctx, bbox } = u;
    if (y < bbox.top || y > bbox.top + bbox.height) return;
    const dpr = u.pxRatio ?? window.devicePixelRatio ?? 1;

    ctx.save();
    ctx.strokeStyle = 'rgba(255,180,84,0.5)';
    ctx.lineWidth = 1 * dpr;
    ctx.setLineDash([4 * dpr, 4 * dpr]);
    ctx.beginPath();
    ctx.moveTo(bbox.left, y);
    ctx.lineTo(bbox.left + bbox.width, y);
    ctx.stroke();
    ctx.setLineDash([]);
    ctx.font = `${10 * dpr}px ui-monospace, SFMono-Regular, Menlo, monospace`;
    ctx.textAlign = 'right';
    ctx.textBaseline = 'bottom';
    const label = `threshold ${this.threshold}`;
    const labelW = ctx.measureText(label).width;
    const right = bbox.left + bbox.width - 4 * dpr;
    // The quiet background runs right through here on a linear axis.
    ctx.fillStyle = 'rgba(17,22,28,0.85)';
    ctx.fillRect(right - labelW - 3 * dpr, y - 14 * dpr, labelW + 6 * dpr, 13 * dpr);
    ctx.fillStyle = 'rgba(255,180,84,0.9)';
    ctx.fillText(label, right, y - 2 * dpr);
    ctx.restore();
  }

  /**
   * Above-threshold stretches as one thin row per sensor under the plot.
   * Full-height vertical shading would be unreadable with several sensors hot
   * at once; a row each keeps "who moved when" unambiguous.
   */
  #drawRibbons() {
    const u = this.u;
    if (!u || !this.ribbonEl || !this.store) return;
    const dpr = u.pxRatio ?? window.devicePixelRatio ?? 1;
    const left = u.bbox.left / dpr;
    const width = u.bbox.width / dpr;
    const [minX, maxX] = u.scales.x.min != null ? [u.scales.x.min, u.scales.x.max] : [0, 1];

    this.ribbonEl.style.paddingLeft = `${left}px`;
    this.ribbonEl.style.paddingRight = `${u.width - left - width}px`;
    this.ribbonEl.innerHTML = '';

    for (const id of this.sensors) {
      const row = document.createElement('div');
      row.className = 'ribbon-row';
      row.title = `${id}: motion_score above threshold`;

      const track = document.createElement('div');
      track.className = 'ribbon-track';
      const spans = this.hidden.has(id) ? [] : this.store.motion.get(id) ?? [];
      for (const span of spans) {
        const a = Math.max(span.from / 1000, minX);
        const b = Math.min(span.to / 1000, maxX);
        if (!(b > a)) continue;
        const seg = document.createElement('span');
        seg.className = 'ribbon-seg';
        seg.style.left = `${((a - minX) / (maxX - minX)) * 100}%`;
        seg.style.width = `${Math.max(0.4, ((b - a) / (maxX - minX)) * 100)}%`;
        seg.style.background = this.palette.colorFor(id);
        track.appendChild(seg);
      }
      row.appendChild(track);
      this.ribbonEl.appendChild(row);
    }
  }

  #resize() {
    if (!this.u) return;
    this.applying = true;
    this.u.setSize({ width: this.el.clientWidth, height: this.el.clientHeight });
    this.applying = false;
    this.#drawRibbons();
  }

  destroy() {
    this._ro.disconnect();
    this.u?.destroy();
    this.u = null;
  }
}
