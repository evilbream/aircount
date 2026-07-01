#pragma once

// Initialises the MQTT client, registers a retained "offline" Last Will
// on <base>/<sensor_id>/status and publishes "online" on connect, then
// starts the client. Call once after Wi-Fi is up.
void mqtt_start(void);

// Publishes a batch JSON to <base>/<sensor_id>/batch at QoS 1.
// Returns the esp-mqtt message id, or -1 on failure. With QoS 1 the
// message is queued in the client outbox and (re)sent on reconnect.
int mqtt_publish_batch(const char *json);
