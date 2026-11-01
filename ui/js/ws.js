// WebSocket client for the aircount.v1 protocol: connect, handshake, subscribe,
// heartbeat, reconnect with jittered backoff.
//
// One session goes:
//
//   server → {"type":"hello","conn_id":…,"heartbeat_ms":25000,…}
//   client → {"op":"subscribe","id":"1","channels":["csi.living-room",…]}
//   server → {"type":"ack","id":"1","op":"subscribe","subscribed":[…]}
//   server → {"type":"event","channel":"csi.living-room","seq":1,"ts":…,"payload":{…}}
//
// Subscriptions live on the connection, not on the server, so every reconnect
// repeats the subscribe. Every parsed message, service ones included, goes to
// onMessage for the journal; only events go on to onEvent.

const MAX_BACKOFF_MS = 30_000;
const BASE_BACKOFF_MS = 500;

// The server offers exactly one subprotocol and hangs up with 1008 on anything
// else, so it has to be asked for by name: an upgrade without it gets a 101
// followed immediately by "client must speak aircount.v1", which looks like a
// working socket in the network tab and delivers nothing.
const SUBPROTOCOL = 'aircount.v1';

// hello is the server's first message. A socket that opens and never says it
// is not speaking this protocol, whatever the 101 claimed.
const HELLO_TIMEOUT_MS = 5_000;
// Used until hello says otherwise.
const DEFAULT_HEARTBEAT_MS = 25_000;
// The browser never sees WebSocket ping/pong frames, so liveness is judged at
// the application level: after heartbeat_ms of silence send {"op":"ping"}, and
// if nothing at all arrives within this much more, the connection is dead.
const PONG_TIMEOUT_MS = 10_000;

export class WSClient {
  /**
   * @param {string}   url        ws:// endpoint
   * @param {string[]} channels   channels to subscribe to on every connect
   * @param {Function} factory    (url, protocol) => WebSocket-like; swapped out in ?mock=1
   * @param {Function} onEvent    (event) => void, for {"type":"event"} only
   * @param {Function} onMessage  (msg, direction) => void, every message both ways
   * @param {Function} onStatus   ({state, retryInMs, attempt, subscribed, error}) => void
   */
  constructor({ url, channels = [], factory = (u, p) => new WebSocket(u, p), onEvent, onMessage, onStatus }) {
    this.url = url;
    this.channels = [...channels];
    this.factory = factory;
    this.onEvent = onEvent;
    this.onMessage = onMessage;
    this.onStatus = onStatus;

    this.ws = null;
    this.attempt = 0;
    this.timer = null;
    this.countdown = null;
    this.helloTimer = null;
    this.heartbeat = null;
    this.stopped = true;

    this.#resetSession();

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
    try { this.ws?.close(1000, 'client stop'); } catch { /* already gone */ }
    this.ws = null;
  }

  connected() { return this.ws && this.ws.readyState === 1; }

  #resetSession() {
    this.nextId = 1;
    this.pending = new Map(); // request id -> op, for matching ack/error
    this.hello = null;
    this.subscribed = [];
    this.lastRxAt = 0;
    this.pingSentAt = null;
    this.byeDelayMs = null;
  }

  #status(state, extra = {}) {
    this.state = state;
    this.onStatus?.({ state, attempt: this.attempt, subscribed: this.subscribed, ...extra });
  }

  #clearTimers() {
    clearTimeout(this.timer); this.timer = null;
    clearInterval(this.countdown); this.countdown = null;
    clearTimeout(this.helloTimer); this.helloTimer = null;
    clearInterval(this.heartbeat); this.heartbeat = null;
  }

  #send(msg) {
    if (!this.connected()) return false;
    this.ws.send(JSON.stringify(msg));
    this.onMessage?.(msg, 'out');
    return true;
  }

  #request(op, fields = {}) {
    const id = String(this.nextId++);
    this.pending.set(id, op);
    this.#send({ op, id, ...fields });
    return id;
  }

  #connectNow() {
    this.#clearTimers();
    // A socket still in CONNECTING counts as not connected, so this can be
    // reached with one in flight; drop it rather than end up with two.
    const old = this.ws;
    this.ws = null;
    try { old?.close(1000, 'reconnect'); } catch { /* already gone */ }
    this.#resetSession();
    this.#status('connecting');

    let ws;
    try {
      ws = this.factory(this.url, SUBPROTOCOL);
    } catch {
      this.#scheduleReconnect();
      return;
    }
    this.ws = ws;

    ws.onopen = () => {
      this.lastRxAt = Date.now();
      this.#status('handshake');
      this.helloTimer = setTimeout(() => this.#kill('no hello from server'), HELLO_TIMEOUT_MS);
    };

    ws.onmessage = (ev) => {
      this.lastRxAt = Date.now();
      this.pingSentAt = null;

      let msg;
      try { msg = JSON.parse(ev.data); } catch {
        this.onMessage?.({ raw: String(ev.data) }, 'in', 'not JSON');
        return;
      }
      this.#dispatch(msg);
    };

    ws.onerror = () => { /* onclose always follows; handle it there */ };

    ws.onclose = () => {
      if (this.ws !== ws) return; // a socket we already gave up on
      this.ws = null;
      this.#clearTimers();
      if (this.stopped) return;
      this.#scheduleReconnect();
    };
  }

  #dispatch(msg) {
    const type = msg?.type;
    // The journal decides how to show it; reasons are only for real protocol
    // violations, not for service messages.
    const reason = typeof type === 'string' ? null : 'no type';
    this.onMessage?.(msg, 'in', reason);
    if (reason) return;

    switch (type) {
      case 'hello': this.#onHello(msg); break;
      case 'ack': this.#onAck(msg); break;
      case 'error': this.#onError(msg); break;
      case 'event': this.onEvent?.(msg); break;
      case 'bye':
        // The server is going away on purpose: wait at least as long as it asked.
        if (Number.isFinite(msg.reconnect_after)) this.byeDelayMs = msg.reconnect_after;
        break;
      case 'pong':
        this.pending.delete(msg.id);
        break;
      case 'dropped':
        break; // lastRxAt already updated; the journal shows them
      default:
        break; // unknown types are ignored, so the server can add new ones
    }
  }

  #onHello(msg) {
    clearTimeout(this.helloTimer); this.helloTimer = null;
    this.hello = msg;
    this.attempt = 0;

    const hb = Number.isFinite(msg.heartbeat_ms) && msg.heartbeat_ms > 0 ? msg.heartbeat_ms : DEFAULT_HEARTBEAT_MS;
    this.heartbeat = setInterval(() => this.#checkHeartbeat(hb), 1000);

    if (!this.channels.length) {
      this.#status('idle');
      return;
    }
    this.#status('subscribing');
    this.#request('subscribe', { channels: this.channels });
  }

  #onAck(msg) {
    const op = this.pending.get(msg.id);
    this.pending.delete(msg.id);
    if (Array.isArray(msg.subscribed)) this.subscribed = msg.subscribed;
    if (op === 'subscribe' || op === 'unsubscribe') {
      this.#status(this.subscribed.length ? 'live' : 'idle');
    }
  }

  #onError(msg) {
    const op = this.pending.get(msg.id);
    this.pending.delete(msg.id);
    // A rejected request is not a dead connection: stay connected, say why.
    if (op === 'subscribe') {
      this.#status('rejected', { error: msg.message ?? String(msg.code ?? 'error') });
    }
  }

  #checkHeartbeat(hbMs) {
    const now = Date.now();
    if (this.pingSentAt != null) {
      if (now - this.pingSentAt > PONG_TIMEOUT_MS) this.#kill('heartbeat timeout');
      return;
    }
    if (now - this.lastRxAt > hbMs) {
      this.pingSentAt = now;
      this.#request('ping');
    }
  }

  // Give up on the current socket without waiting for its close handshake: a
  // dead peer would never complete it.
  #kill(reason) {
    const ws = this.ws;
    this.ws = null;
    this.#clearTimers();
    try { ws?.close(4000, reason); } catch { /* already gone */ }
    if (!this.stopped) this.#scheduleReconnect();
  }

  /** Replace the channel set; applied right away when live, and on every reconnect. */
  setChannels(channels) {
    const next = [...new Set(channels)];
    const drop = this.channels.filter((c) => !next.includes(c));
    const add = next.filter((c) => !this.channels.includes(c));
    this.channels = next;
    if (!this.hello) return; // not handshaken yet: hello will subscribe to the new set
    if (drop.length) this.#request('unsubscribe', { channels: drop });
    if (add.length) this.#request('subscribe', { channels: add });
  }

  #scheduleReconnect() {
    if (document.hidden) {
      this.#status('reconnecting', { retryInMs: null });
      return;
    }
    // Full jitter: without it every open tab reconnects in lockstep the moment
    // the server comes back and hits it as one thundering herd.
    const ceiling = Math.min(MAX_BACKOFF_MS, BASE_BACKOFF_MS * 2 ** this.attempt);
    let delay = Math.round(ceiling * (0.5 + Math.random() * 0.5));
    if (this.byeDelayMs != null) delay = Math.max(delay, this.byeDelayMs);
    this.attempt++;

    let left = delay;
    this.#status('reconnecting', { retryInMs: left });
    this.countdown = setInterval(() => {
      left = Math.max(0, left - 1000);
      this.#status('reconnecting', { retryInMs: left });
    }, 1000);

    this.timer = setTimeout(() => this.#connectNow(), delay);
  }
}
