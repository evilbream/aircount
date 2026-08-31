# 👻 Aircount

**Counting people in a room over Wi-Fi — no cameras, no microphones, no wearables.**

Device-free people counting over Wi-Fi: ESP32 sensors stream CSI data to a Go pipeline that detects motion and estimates room occupancy — no cameras, no wearables.

## How it works

```
[ESP32 × N]                     Go services                       Kafka topics
 CSI + sniffer ──MQTT──▶ ingest-gateway ──protobuf──▶ raw.csi / raw.detections
                                                          │
                                          csi-processor ──┤  CSI window → motion?
                                              normalizer ─┤  MAC → person (device registry)
                                              aggregator ─┘  fan-in → how many people are home
                                                          │
                                             api-service ──▶ REST + WebSocket dashboard
```

- **Firmware** (ESP-IDF, C): captures CSI frames and probe requests, batches them, and ships over MQTT.

## Stack

Go · Apache Kafka (franz-go) · Protobuf + Schema Registry · MQTT (Mosquitto) · ESP-IDF · Docker Compose

