// uPlot only listens to the mouse, so without this the chart is a picture on a
// phone: no values under the finger and no way to zoom. One finger scrubs the
// cursor, two pinch the time axis.
//
// A vertical swipe stays the browser's (touch-action: pan-y in app.css): the
// chart fills most of the screen and the page still has to scroll past it.

const LINGER_MS = 2000; // the readout stays this long after the finger lifts
const MIN_SPAN_S = 60;
const SCROLL_SLOP_PX = 10;

const clamp = (v, lo, hi) => Math.min(hi, Math.max(lo, v));

/**
 * @param {HTMLElement} el      the chart container; uPlot is rebuilt inside it
 * @param {() => object} plot   the current uPlot instance
 * @param {(full: boolean) => void} onZoom  full: pinched back out to everything
 */
export function bindTouch(el, { plot, onZoom }) {
  let pinch = null;
  let linger = null;
  let start = null; // where the finger came down, until the gesture proves to be a scroll

  const hideCursor = () => plot()?.setCursor({ left: -10, top: -10 });

  function scrub(u, touch) {
    const box = u.over.getBoundingClientRect();
    u.setCursor({
      left: clamp(touch.clientX - box.left, 0, box.width),
      top: clamp(touch.clientY - box.top, 0, box.height),
    });
  }

  function fingers(u, touches) {
    const box = u.over.getBoundingClientRect();
    const [a, b] = [touches[0].clientX, touches[1].clientX];
    return {
      mid: clamp((a + b) / 2 - box.left, 0, box.width),
      gap: Math.max(Math.abs(a - b), 1),
      width: box.width,
    };
  }

  function zoom(u, touches) {
    const xs = u.data[0];
    if (xs.length < 2) return;
    const first = xs[0];
    const last = xs[xs.length - 1];
    const { mid, gap, width } = fingers(u, touches);

    const span = clamp(pinch.span * (pinch.gap / gap), MIN_SPAN_S, last - first);
    // Whatever was between the fingers stays between them.
    const min = clamp(pinch.at - span * (mid / width), first, last - span);
    u.setScale('x', { min, max: min + span });
    onZoom(span >= last - first);
  }

  el.addEventListener('touchstart', (e) => {
    const u = plot();
    if (!u) return;
    clearTimeout(linger);
    if (e.touches.length === 1) {
      start = { x: e.touches[0].clientX, y: e.touches[0].clientY };
      scrub(u, e.touches[0]);
      return;
    }
    start = null;
    hideCursor();
    const { mid, gap } = fingers(u, e.touches);
    pinch = { gap, span: u.scales.x.max - u.scales.x.min, at: u.posToVal(mid, 'x') };
  }, { passive: true });

  el.addEventListener('touchmove', (e) => {
    const u = plot();
    if (!u) return;
    if (pinch) {
      if (e.touches.length > 1) zoom(u, e.touches);
      return;
    }
    if (!start) return;
    const touch = e.touches[0];
    const dy = Math.abs(touch.clientY - start.y);
    // Chrome keeps sending touchmove while it scrolls and never sends touchcancel,
    // so the readout would ride along with the page.
    if (dy > Math.max(SCROLL_SLOP_PX, Math.abs(touch.clientX - start.x))) {
      start = null;
      hideCursor();
      return;
    }
    scrub(u, touch);
  }, { passive: true });

  function end(e) {
    if (e.touches.length) return;
    const scrolled = !pinch && !start;
    pinch = null;
    start = null;
    // A tap is followed by synthetic mouse events. uPlot would take them for a
    // hover, and with no mouse to leave the plot that cursor would never go away.
    if (e.cancelable) e.preventDefault();
    if (!scrolled) linger = setTimeout(hideCursor, e.type === 'touchcancel' ? 0 : LINGER_MS);
  }

  el.addEventListener('touchend', end, { passive: false });
  el.addEventListener('touchcancel', end);
}
