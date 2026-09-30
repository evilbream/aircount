# Aircount UI

Activity dashboard: one `motion_score` line per `sensor_id`, over
5m/10m/30m/1h/6h/24h windows. Plain ES modules built by Vite; the only runtime
dependency is `uplot` (v1.6.32, MIT). `npm run build` produces `dist/` — a
static folder any host can serve, with no Go service behind it.

## Contract

The server sends **one flat object per CSI window**, with no envelope and no
type tag — exactly what `uiwire.UIWireEv` marshals into:

```json
{"sensor_id":"living-room","timestamp":1756713600000,"motion_score":0.0453}
```

`timestamp` is unix ms (`ObservedAt`, stamped by the gateway). **All sensors
share one connection**; the split into series by `sensor_id` is entirely
client-side — a new `sensor_id` is picked up on the fly, and its series and
color appear on their own.

The stream is one-way: the client sends nothing, there is no subscription and
the socket has no handshake. A message without `sensor_id` or without a numeric
`motion_score` is ignored rather than fatal — the server can start sending more
without breaking an already-open tab.

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

`npm run dev` proxies `/ws` to `http://localhost:8088`, so the page and the
socket share an origin: no `?ws=` parameter and no cross-origin handshake.
Point it at another backend with `VITE_API_ORIGIN=http://host:port npm run dev`.

**Without a backend** — a synthetic stream in the same format:

```
http://localhost:5173/?mock=1          live stream, one window per sensor every 5s
http://localhost:5173/?mock=1&seed=1   + 2 hours backdated, to check 6h/24h right away
http://localhost:5173/?mock=1&flaky=1  + a dropped connection at the 45-second mark
```

More parameters: `?range=6h`, `?scale=linear`, `?ws=wss://host/ws` — the view is
shareable by link.

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
reverse proxy puts the UI and `/ws` behind one hostname. Precedence:
`?ws=` (one tab) > `config.js` > same origin.

If the API is on another origin, `websocket.Accept` has to allow it:
`&websocket.AcceptOptions{OriginPatterns: []string{"ui.example.com"}}`
(`internal/httpapi/ws.go` currently passes `InsecureSkipVerify: true`, which
allows any).

The Y axis is **logarithmic** by default: in real captures `motion_score` runs
from 0.002 to 19.7, and on a linear axis everything normal collapses into a line
along zero. The `log`/`linear` toggle is in the top right corner.

## Files

| | |
|---|---|
| `js/series.js` | raw point store, bucketing, palette. Pure functions |
| `js/chart.js`  | uPlot, follow mode, the "no data" zone, threshold line, ribbons |
| `js/ws.js`     | connect, backoff with jitter, pause on a hidden tab |
| `js/app.js`    | state, controls, statuses |
| `js/mock.js`   | `?mock=1` — a fake socket in the same format |

To serve the UI from `api-service` instead of a separate host, embed the build
output: `//go:embed all:dist` after `npm run build`, and leave `config.js` with
an empty `wsURL` so the page uses its own origin.
