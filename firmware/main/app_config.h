#pragma once

// Thin aliases over the Kconfig values (set via `idf.py menuconfig`,
// menu "AirCount Sensor Configuration"). Keeps the rest of the code
// free of CONFIG_ prefixes.

#define CFG_WIFI_SSID      CONFIG_AIRCOUNT_WIFI_SSID
#define CFG_WIFI_PASSWORD  CONFIG_AIRCOUNT_WIFI_PASSWORD
#define CFG_SENSOR_ID      CONFIG_AIRCOUNT_SENSOR_ID
#define CFG_MQTT_URI        CONFIG_AIRCOUNT_MQTT_URI
#define CFG_MQTT_TOPIC_BASE CONFIG_AIRCOUNT_MQTT_TOPIC_BASE
#define CFG_WINDOW_MS      CONFIG_AIRCOUNT_WINDOW_MS
#define CFG_MAX_DEVICES    CONFIG_AIRCOUNT_MAX_DEVICES
