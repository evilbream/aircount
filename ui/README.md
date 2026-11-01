# Aircount UI

Activity dashboard: one `motion_score` line per `sensor_id`, over
5m/10m/30m/1h/6h/24h windows. Plain ES modules built by Vite; the only runtime
dependency is `uplot` (v1.6.32, MIT). `npm run build` produces `dist/` — a
static folder any host can serve, with no Go service behind it.

## Contract

Subprotocol `aircount.v1` (types in `internal/uiwire`). One session:

```
← {"type":"hello","conn_id":"…","server_ts":…,"heartbeat_ms":25000,"limits":{…}}
→ {"op":"subscribe","id":"1","channels":["csi.living-room","csi.bedroom"]}
← {"type":"ack","id":"1","op":"subscribe","subscribed":["csi.bedroom","csi.living-room"]}
← {"type":"event","channel":"csi.living-room","seq":42,"ts":1756713600000,
   "payload":{"sensor_id":"living-room","timestamp":1756713600000,"motion_score":0.0453}}
```

**Sensors come from the server**: on load the tab calls `GET /list` on the same
host as `/ws`, gets `["living-room","bedroom"]` and subscribes to one
`csi.<sensor_id>` per sensor. If that fails, the status line says so and it
retries every 5 s. The list is read once per load, so a new sensor needs a
reload. `?sensors=living-room,bedroom` skips the call and pins the set for one
tab. The hub has no wildcards, so a sensor that is not listed is not shown.
Subscriptions belong to the connection: every reconnect repeats them.

`ts` is unix ms of the observation and is what the chart plots by; `seq` grows
per channel, so a gap means a lost event on that channel. The browser cannot see
WebSocket ping frames, so after `heartbeat_ms` of silence the tab sends
`{"op":"ping"}` and expects `pong`; nothing within 10 s more means a dead
connection and a reconnect.

An event without a `csi.` channel, `payload.sensor_id` or a numeric
`payload.motion_score` is ignored rather than fatal, and unknown message types
are skipped — the server can add more without breaking an already-open tab. The
journal shows every message both ways, service ones dimmed, rejected ones with
the reason.

### What follows from that

**There is no history.** The server only sends new windows, so the chart knows
only what this tab has collected since it connected. The 6h/24h windows fill in
as the tab keeps running and reset on reload. The UI says so outright: the chip
reads "collecting since 12:18 · 40m of data", and everything to the left of the
connection point is a hatched "no data — the API started here" zone. Without it,
an empty 24h axis would read as "there was no motion" — a lying dashboard.

**Switching the window is local.** The tab keeps the raw points (up to 24h) and
rebuilds the grid for the selected window; nothing goes to the server.

**Bucketing happens on the client**, `bucketMs = max(5s, ceilTo5s(rangeMs / 360))`
→ 5m/10m/30m: 5s, 1h: 10s, 6h: 60s, 24h: 240s. The aggregate is **max**, not the
mean: the feature is about spikes, and a mean over a 4-minute bucket would bury a
single 19.7 peak under 47 quiet samples. A marker dot on the line means the
bucket was built from noticeably fewer samples than it could hold.

**The "above threshold" ribbons are computed here.** `presence.rf` does not
arrive on this socket, so the motion stretches are derived with the same `0.06`
threshold as `domain.presenceMotionThreshold`. If you start sending the
detector's verdict, say so and I'll switch to it.

## Running it

Node 20+ (checked on 24). Everything happens inside `ui/`:

```bash
npm install
npm run dev        # http://localhost:5173, hot reload
npm run build      # -> dist/
npm run preview    # serve dist/ on :4173, to check the real build
```

`npm run dev` proxies `/ws` and `/list` to `http://localhost:8088`, so the page
and the API share an origin: no `?ws=` parameter and no cross-origin handshake.
Point it at another backend with `VITE_API_ORIGIN=http://host:port npm run dev`.

**Without a backend** — a fake socket speaking the same protocol; it accepts any
`csi.<sensor_id>`, so `?sensors=` decides what it emits (`/list` is not called;
without `?sensors=` it shows `MOCK_SENSORS` from `js/mock.js`):

```
http://localhost:5173/?mock=1          live stream, one window per sensor every 5s
http://localhost:5173/?mock=1&seed=1   + 2 hours backdated, to check 6h/24h right away
http://localhost:5173/?mock=1&flaky=1  + a dropped connection at the 45-second mark
```

More parameters: `?sensors=living-room,bedroom`, `?range=6h`, `?scale=linear`,
`?ws=wss://host/ws` — the view is shareable by link.

### Deploying it

`dist/` is the whole artifact: upload it to any static host, no Node at runtime.
The one file that differs between environments is `public/config.js`, which is
copied into `dist/` **verbatim** (Vite never bundles `public/`), so one build
works everywhere — the host overwrites `dist/config.js`, or a container
generates it from env at start:

```js
window.AIRCOUNT_CONFIG = { wsURL: 'wss://api.example.com/ws' };
```

An empty `wsURL` means "same origin as the page" — that is the mode where a
reverse proxy puts the UI, `/ws` and `/list` behind one hostname. Precedence:
`?ws=` (one tab) > `config.js` > same origin.

If the API is on another origin, `websocket.Accept` has to allow it:
`&websocket.AcceptOptions{OriginPatterns: []string{"ui.example.com"}}`
(`internal/httpapi/ws.go` currently passes `InsecureSkipVerify: true`, which
allows any). `/list` answers with `Access-Control-Allow-Origin: *` for the same
reason; narrow both together.

The Y axis is **logarithmic** by default: in real captures `motion_score` runs
from 0.002 to 19.7, and on a linear axis everything normal collapses into a line
along zero. The `log`/`linear` toggle is in the top right corner.

## Files

| | |
|---|---|
| `js/series.js` | raw point store, bucketing, palette. Pure functions |
| `js/chart.js`  | uPlot, follow mode, the "no data" zone, threshold line, ribbons |
| `js/ws.js`     | aircount.v1 session: hello, subscribe/ack, ping/pong, backoff with jitter, pause on a hidden tab |
| `js/app.js`    | state, controls, statuses, which sensors to subscribe to |
| `js/mock.js`   | `?mock=1` — a fake socket speaking the same protocol |

To serve the UI from `api-service` instead of a separate host, embed the build
output: `//go:embed all:dist` after `npm run build`, and leave `config.js` with
an empty `wsURL` so the page uses its own origin.
