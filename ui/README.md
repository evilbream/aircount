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

**Sensors come from the server**: on load the tab calls `GET /api/list`, next to
`/api/ws`, gets `["living-room","bedroom"]` and subscribes to one
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

**History comes from ClickHouse**, next to `/api/ws` as well:

```
GET /api/history?sensor=living-room,bedroom&from=1756710000000&to=1756713600000
→ {"sensor_ids":["living-room","bedroom"],"from":…,"to":…,
   "history":[{"sensor_id":"living-room","timestamp":1756710003000,"motion_score":0.0453},…]}
```

`from`/`to` are unix ms, `[from, to)`; `history` is the same `uiwire.UIWireEv` as
an event's payload, ordered by time. The tab always passes `sensor`, because
without it the server answers for every sensor, including ones the socket is not
subscribed to.

### What follows from that

**History and live are one set of points.** Both carry the server's
`observed_at` in ms (`DateTime64(3)`), so the store keys points by
`(sensor_id, timestamp)` and the overlap between the two simply collapses.

**History is loaded per window.** On load the tab asks for the selected window;
picking a wider one asks only for the part before what is already held (1h →
24h fetches the 23h before), and a narrower one asks for nothing. The tab keeps
the raw points (up to 24h) and rebuilds the grid for the selected window. While
a request is in flight, or if it fails, the line above the chart says so and the
axis is fitted to the data that is there, with the hatched "no data" zone to the
left. A failed request is retried every 5 s.

**ClickHouse lags the stream.** The writer flushes every 30 s and only once 50
rows have piled up, so the last ~2 minutes (three sensors) to ~5 (one) are not in
`/history` yet. A stretch the socket did not deliver, such as the tail before the
first connect or a dropped connection, is asked for again at 35 s, 2 min and 5 min,
after which it must have landed. Until then it is a visible hole, not a flat line.

**The sensor cards** take "motion N ago" from history, so it is known at once;
the state (Motion/Quiet/Silent) still waits for the first live point, since the
last point in ClickHouse is minutes old by construction.

**Bucketing happens on the client**, `bucketMs = max(5s, ceilTo5s(rangeMs / 360))`
→ 5m/10m/30m: 5s, 1h: 10s, 6h: 60s, 24h: 240s. The aggregate is **max**, not the
mean: the feature is about spikes, and a mean over a 4-minute bucket would bury a
single 19.7 peak under 47 quiet samples. A marker dot on the line means the
bucket was built from noticeably fewer samples than it could hold.

**The "above threshold" ribbons are computed here.** `presence.rf` does not
arrive on this socket, so the motion stretches are derived with the same `0.06`
threshold as `domain.presenceMotionThreshold`. If you start sending the
detector's verdict, say so and I'll switch to it.

**The sensor cards use the latest raw window**, not a chart bucket, so the state
does not depend on the selected range: Motion or Quiet against the same `0.06`,
Silent after 15 s without data, Waiting before the first point. Tapping a card
hides the sensor on the chart, and the axis refits to the ones that are left.

**The chart works by touch**: one finger reads the values, two pinch the time
axis, and a vertical swipe scrolls the page as usual.

## Running it

Node 20+ (checked on 24). Everything happens inside `ui/`:

```bash
npm install
npm run dev        # http://localhost:5173, hot reload
npm run build      # -> dist/
npm run preview    # serve dist/ on :4173, to check the real build
```

`npm run dev` proxies `/api` to `http://localhost:8088` and strips the prefix, as
nginx does in deploy, so the page and the API share an origin: no `?ws=`
parameter and no cross-origin handshake.
Point it at another backend with `VITE_API_ORIGIN=http://host:port npm run dev`.

**Without a backend** — a fake socket speaking the same protocol plus a fake
`/history` built from the same generator; it accepts any `csi.<sensor_id>`, so
`?sensors=` decides what it emits (`/api/list` is not called; without
`?sensors=` it shows `MOCK_SENSORS` from `js/mock.js`):

```
http://localhost:5173/?mock=1          history for the window + one window per sensor every 5s
http://localhost:5173/?mock=1&seed=1   + 2 hours backdated over the socket itself
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
reverse proxy puts the UI and `/api/` behind one hostname and strips the prefix
(`deploy/nginx/default.conf`). Precedence:
`?ws=` (one tab) > `config.js` > same origin.

If the API is on another origin, `websocket.Accept` has to allow it:
`&websocket.AcceptOptions{OriginPatterns: []string{"ui.example.com"}}`
(`internal/httpapi/ws.go` currently passes `InsecureSkipVerify: true`, which
allows any). `/list` and `/history` answer with `Access-Control-Allow-Origin: *`
for the same reason; narrow all of them together.

The Y axis is linear by default. In real captures `motion_score` runs from 0.002
to 19.7, so one spike collapses everything normal into a line along zero: switch
to `Log` under the chart, or open the page with `?scale=log`.

## Files

| | |
|---|---|
| `js/series.js` | raw point store (history and live merged by timestamp), bucketing, palette. Pure functions |
| `js/chart.js`  | uPlot, follow mode, the "no data" zone, threshold line, ribbons, cursor readout |
| `js/touch.js`  | finger input for the chart: scrubbing and pinch zoom |
| `js/rooms.js`  | one card per sensor with its current state |
| `js/journal.js`| on-screen log of the frames going over the socket |
| `js/ws.js`     | aircount.v1 session: hello, subscribe/ack, ping/pong, backoff with jitter, pause on a hidden tab |
| `js/app.js`    | state, controls, statuses, which sensors to subscribe to, `/history` loading and gap refill |
| `js/mock.js`   | `?mock=1` — a fake socket and `/history` speaking the same protocol |

To serve the UI from `api-service` instead of a separate host, embed the build
output: `//go:embed all:dist` after `npm run build`, and set `wsURL` in
`config.js` to that host's `/ws`, because the API has no `/api` prefix of its own.
