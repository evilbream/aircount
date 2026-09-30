// WebSocket client: connect, reconnect with jittered backoff, hand frames on.
//
// The stream is one-way and unsubscribed: the server pushes every CSI window
// from every sensor down one connection, and the client splits by sensor_id.
// The only thing negotiated is the subprotocol below; there is no handshake
// message on top of it.

const MAX_BACKOFF_MS = 30_000;
const BASE_BACKOFF_MS = 500;

// The server offers exactly one subprotocol and hangs up with 1008 on anything
// else, so it has to be asked for by name: an upgrade without it gets a 101
// followed immediately by "client must speak aircount.v1", which looks like a
// working socket in the network tab and delivers nothing.
const SUBPROTOCOL = 'aircount.v1';

export class WSClient {
  /**
   * @param {string}   url      ws:// endpoint
   * @param {Function} factory  (url, protocol) => WebSocket-like; swapped out in ?mock=1
   * @param {Function} onFrame  (frame) => void
   * @param {Function} onStatus ({state, retryInMs, attempt}) => void
   */
  constructor({ url, factory = (u, p) => new WebSocket(u, p), onFrame, onStatus }) {
    this.url = url;
    this.factory = factory;
    this.onFrame = onFrame;
    this.onStatus = onStatus;

    this.ws = null;
    this.attempt = 0;
    this.timer = null;
    this.countdown = null;
    this.stopped = true;

    document.addEventListener('visibilitychange', () => {
      // Background tabs get their timers throttled, so a hidden tab would wake
      // up holding a stale chart. Don't burn reconnects while hidden; reconnect
      // the instant we're visible again.
      if (document.hidden) return;
      if (!this.stopped && !this.connected()) this.#connectNow();
    });
  }

  start() { this.stopped = false; this.#connectNow(); }

  stop() {
    this.stopped = true;
    this.#clearTimers();
    try { this.ws?.close(); } catch { /* already gone */ }
    this.ws = null;
  }

  connected() { return this.ws && this.ws.readyState === 1; }

  #clearTimers() {
    clearTimeout(this.timer); this.timer = null;
    clearInterval(this.countdown); this.countdown = null;
  }

  #connectNow() {
    this.#clearTimers();
    this.onStatus?.({ state: 'connecting', attempt: this.attempt });

    let ws;
    try {
      ws = this.factory(this.url, SUBPROTOCOL);
    } catch {
      this.#scheduleReconnect();
      return;
    }
    this.ws = ws;

    ws.onopen = () => {
      this.attempt = 0;
      this.onStatus?.({ state: 'live', attempt: 0 });
    };

    ws.onmessage = (ev) => {
      let frame;
      try { frame = JSON.parse(ev.data); } catch { return; }
      this.onFrame?.(frame);
    };

    ws.onerror = () => { /* onclose always follows; handle it there */ };

    ws.onclose = () => {
      this.ws = null;
      if (this.stopped) return;
      this.#scheduleReconnect();
    };
  }

  #scheduleReconnect() {
    if (document.hidden) {
      this.onStatus?.({ state: 'reconnecting', retryInMs: null, attempt: this.attempt });
      return;
    }
    // Full jitter: without it every open tab reconnects in lockstep the moment
    // the server comes back and hits it as one thundering herd.
    const ceiling = Math.min(MAX_BACKOFF_MS, BASE_BACKOFF_MS * 2 ** this.attempt);
    const delay = Math.round(ceiling * (0.5 + Math.random() * 0.5));
    this.attempt++;

    let left = delay;
    this.onStatus?.({ state: 'reconnecting', retryInMs: left, attempt: this.attempt });
    this.countdown = setInterval(() => {
      left = Math.max(0, left - 1000);
      this.onStatus?.({ state: 'reconnecting', retryInMs: left, attempt: this.attempt });
    }, 1000);

    this.timer = setTimeout(() => this.#connectNow(), delay);
  }
}
