# AirCount sensor firmware (ESP32-S3, ESP-IDF)

Firmware for the Wi-Fi presence sensor. One ESP32-S3 per room. It
associates with the home Wi-Fi and, on that channel, runs two
independent signal sources at once:

- **MAC sniffer** — promiscuous capture of management + data frames →
  unique device MACs with RSSI (the *who*).
- **CSI / Wi-Fi sensing** — variance of channel-state amplitude over a
  window → a motion score (the *is-someone-moving*, even with no phone).

Every `WINDOW_MS` it batches both and **publishes** a JSON payload over
MQTT (QoS 1) to a Mosquitto broker; `ingest-gateway` will later
subscribe and produce into Kafka.

> Why associate as STA instead of channel-hopping: capturing CSI and
> sending HTTP both require being on one channel with an IP. Associating
> pins us to the home AP's channel and still lets the sniffer see probe
> requests and frames associated with that AP — which is exactly the
> traffic we care about at home.

## Layout

```
firmware/
├── CMakeLists.txt          # IDF project
├── sdkconfig.defaults      # enables CONFIG_ESP_WIFI_CSI_ENABLED
└── main/
    ├── Kconfig.projbuild    # SSID / password / sensor_id / URL / window
    ├── app_config.h         # aliases over the Kconfig values
    ├── main.c               # init order: wifi → mqtt → sniffer → csi → batch
    ├── wifi.c/.h            # STA connect, channel lock
    ├── sniffer.c/.h         # promiscuous MAC table
    ├── csi.c/.h             # CSI amplitude-variance motion score
    ├── mqtt.c/.h            # MQTT client, Last Will, QoS 1 publish
    └── batch.c/.h           # window drain → JSON → MQTT publish
```

## Build & flash

Requires ESP-IDF **v5.1–v5.2** (`. $IDF_PATH/export.sh`).

```bash
cd firmware
idf.py set-target esp32s3
idf.py menuconfig          # → "AirCount Sensor Configuration": SSID,
                           #   password, sensor_id, MQTT broker URI, window
idf.py build
idf.py -p /dev/ttyACM0 flash monitor
```

> **IDF version note:** `wifi_csi_config_t` changed field names in IDF
> v5.3+. This builds against the v5.1/v5.2 struct (`lltf_en`, `htltf_en`,
> …). On v5.3+ adjust the initializer in `csi.c`.

## Transport & topics (MQTT)

| Topic | QoS | Retain | Payload |
|---|---|---|---|
| `aircount/<sensor_id>/batch`  | 1 | no  | the JSON batch below, once per window |
| `aircount/<sensor_id>/status` | 1 | yes | `online` / `offline` (offline is the Last Will) |

The base (`aircount`) and broker URI are set in `menuconfig`. The
retained status topic + Last Will mean any subscriber instantly sees a
sensor as `offline` if it loses power or drops off — no polling.

> **At-least-once:** QoS 1 means a window may be delivered more than once
> (e.g. on reconnect). Downstream must treat batches idempotently — which
> the Kafka pipeline already does (idempotent upsert in `sink-consumer`).
> If the broker is unreachable, QoS 1 batches queue in the client's RAM
> outbox and flush on reconnect, but are lost on a power cycle — add a
> flash-backed queue if that matters.

## Data contract (batch payload)

```json
{
  "sensor_id": "living-room",
  "uptime_ms": 123456,
  "window_ms": 5000,
  "devices": [
    { "mac": "aa:bb:cc:dd:ee:ff", "rssi": -52, "channel": 6,
      "frames": 12, "random": false }
  ],
  "csi": { "packets": 48, "motion_score": 0.137, "subcarriers": 64 }
}
```

- The device has no RTC: it sends `uptime_ms` only; the gateway stamps
  the authoritative ingest time.
- `random: true` means the MAC has the locally-administered bit set
  (randomized). Stable home MACs (used when a phone is associated to the
  home AP) come through with `random: false` — those are the ones the
  registry maps to people.
- `motion_score` is raw amplitude variance; thresholding lives
  downstream in `csi-processor`.

This JSON is the same shape `sensor-sim` should emit, so the backend
can be developed against the simulator and the real board
interchangeably.

## Testing without the backend

`ingest-gateway` does not exist yet, but the broker does. Start it and
subscribe — you'll see real batches from the board with no Kafka needed:

```bash
cd ../deploy && docker compose up -d mosquitto

# watch everything the sensors publish (batches + online/offline)
mosquitto_sub -h localhost -t 'aircount/#' -v
```

Point the board's `AIRCOUNT_MQTT_URI` at `mqtt://<your-host-ip>:1883`
(the machine running Mosquitto, not `localhost` — that's the ESP32's
own loopback). `idf.py monitor` also logs device + CSI counts and the
MQTT message id every window.
