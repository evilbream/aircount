#include "mqtt.h"
#include "app_config.h"

#include <stdio.h>
#include <string.h>

#include "mqtt_client.h"
#include "esp_log.h"

static const char *TAG = "mqtt";

static esp_mqtt_client_handle_t s_client;
static char s_batch_topic[96];   // <base>/<sensor_id>/batch
static char s_status_topic[96];  // <base>/<sensor_id>/status

static void on_mqtt_event(void *handler_args, esp_event_base_t base,
                          int32_t event_id, void *event_data)
{
    switch ((esp_mqtt_event_id_t)event_id) {
    case MQTT_EVENT_CONNECTED:
        ESP_LOGI(TAG, "connected to broker");
        // Retained "online" clears the Last Will until we drop off.
        esp_mqtt_client_publish(s_client, s_status_topic,
                                "online", 0, /*qos*/ 1, /*retain*/ 1);
        break;
    case MQTT_EVENT_DISCONNECTED:
        ESP_LOGW(TAG, "disconnected from broker");
        break;
    case MQTT_EVENT_ERROR:
        ESP_LOGW(TAG, "mqtt error");
        break;
    default:
        break;
    }
}

void mqtt_start(void)
{
    snprintf(s_batch_topic, sizeof(s_batch_topic), "%s/%s/batch",
             CFG_MQTT_TOPIC_BASE, CFG_SENSOR_ID);
    snprintf(s_status_topic, sizeof(s_status_topic), "%s/%s/status",
             CFG_MQTT_TOPIC_BASE, CFG_SENSOR_ID);

    esp_mqtt_client_config_t cfg = {
        .broker.address.uri = CFG_MQTT_URI,
        // Last Will: if the sensor drops without a clean disconnect, the
        // broker publishes this retained -> downstream sees it offline.
        .session.last_will.topic  = s_status_topic,
        .session.last_will.msg    = "offline",
        .session.last_will.msg_len = 0,   // 0 => strlen
        .session.last_will.qos    = 1,
        .session.last_will.retain = 1,
    };

    s_client = esp_mqtt_client_init(&cfg);
    esp_mqtt_client_register_event(s_client, ESP_EVENT_ANY_ID,
                                   &on_mqtt_event, NULL);
    esp_mqtt_client_start(s_client);
    ESP_LOGI(TAG, "mqtt client started, batch topic \"%s\"", s_batch_topic);
}

int mqtt_publish_batch(const char *json)
{
    return esp_mqtt_client_publish(s_client, s_batch_topic,
                                   json, 0, /*qos*/ 1, /*retain*/ 0);
}
