#include "batch.h"
#include "app_config.h"
#include "sniffer.h"
#include "csi.h"
#include "mqtt.h"

#include <stdio.h>
#include <stdlib.h>
#include <assert.h>

#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "esp_timer.h"
#include "esp_log.h"
#include "cJSON.h"

static const char *TAG = "batch";

static char *build_json(const device_t *devs, size_t n,
                        const csi_window_t *csi)
{
    cJSON *root = cJSON_CreateObject();
    cJSON_AddStringToObject(root, "sensor_id", CFG_SENSOR_ID);
    // No RTC on the device; the gateway assigns the authoritative
    // timestamp. uptime_ms lets it sanity-check ordering per sensor.
    cJSON_AddNumberToObject(root, "uptime_ms",
                            (double)(esp_timer_get_time() / 1000));
    cJSON_AddNumberToObject(root, "window_ms", CFG_WINDOW_MS);

    cJSON *arr = cJSON_AddArrayToObject(root, "devices");
    for (size_t i = 0; i < n; i++) {
        char mac[18];
        snprintf(mac, sizeof(mac), "%02x:%02x:%02x:%02x:%02x:%02x",
                 devs[i].mac[0], devs[i].mac[1], devs[i].mac[2],
                 devs[i].mac[3], devs[i].mac[4], devs[i].mac[5]);
        cJSON *d = cJSON_CreateObject();
        cJSON_AddStringToObject(d, "mac", mac);
        cJSON_AddNumberToObject(d, "rssi", devs[i].rssi);
        cJSON_AddNumberToObject(d, "channel", devs[i].channel);
        cJSON_AddNumberToObject(d, "frames", devs[i].frames);
        cJSON_AddBoolToObject(d, "random", devs[i].random_mac);
        cJSON_AddItemToArray(arr, d);
    }

    cJSON *c = cJSON_AddObjectToObject(root, "csi");
    cJSON_AddNumberToObject(c, "packets", csi->packets);
    cJSON_AddNumberToObject(c, "motion_score", csi->motion_score);
    cJSON_AddNumberToObject(c, "subcarriers", csi->subcarriers);

    char *out = cJSON_PrintUnformatted(root);
    cJSON_Delete(root);
    return out;
}

static void batch_task(void *arg)
{
    device_t *devs = calloc(CFG_MAX_DEVICES, sizeof(device_t));
    assert(devs);

    for (;;) {
        vTaskDelay(pdMS_TO_TICKS(CFG_WINDOW_MS));

        size_t n = sniffer_collect(devs, CFG_MAX_DEVICES);
        csi_window_t csi;
        csi_collect(&csi);

        char *body = build_json(devs, n, &csi);
        if (!body) continue;
        int msg_id = mqtt_publish_batch(body);
        ESP_LOGI(TAG, "window: %u devices, %u csi pkts, motion=%.3f (mqtt id %d)",
                 (unsigned)n, (unsigned)csi.packets, csi.motion_score, msg_id);
        cJSON_free(body);
    }
}

void batch_start(void)
{
    xTaskCreate(batch_task, "batch", 8192, NULL, 5, NULL);
}
