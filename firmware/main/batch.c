#include "batch.h"
#include "app_config.h"
#include "sniffer.h"
#include "csi.h"
#include "mqtt.h"

#include <stdio.h>
#include <stdlib.h>
#include <inttypes.h>
#include <assert.h>

#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "esp_timer.h"
#include "esp_log.h"
#include "cJSON.h"

static const char *TAG = "batch";

static void format_mac(char out[18], const uint8_t *m)
{
    snprintf(out, 18, "%02x:%02x:%02x:%02x:%02x:%02x",
             m[0], m[1], m[2], m[3], m[4], m[5]);
}

static char *build_json(const device_t *devs, size_t n,
                        const device_t *relayed, size_t n_relayed,
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
        format_mac(mac, devs[i].mac);
        cJSON *d = cJSON_CreateObject();
        cJSON_AddStringToObject(d, "mac", mac);
        cJSON_AddNumberToObject(d, "rssi", devs[i].rssi);
        cJSON_AddNumberToObject(d, "channel", devs[i].channel);
        cJSON_AddNumberToObject(d, "frames", devs[i].frames);
        cJSON_AddBoolToObject(d, "random", devs[i].random_mac);
        if (devs[i].fp) {
            char fp[9];
            snprintf(fp, sizeof(fp), "%08" PRIx32, devs[i].fp);
            cJSON_AddStringToObject(d, "fp", fp);
        }
        cJSON_AddItemToArray(arr, d);
    }

    // Hosts our AP forwarded traffic from (5 GHz clients, wired hosts).
    // No rssi/channel: the radio signal is the AP's, not the host's.
    cJSON *rel = cJSON_AddArrayToObject(root, "relayed");
    for (size_t i = 0; i < n_relayed; i++) {
        char mac[18];
        format_mac(mac, relayed[i].mac);
        cJSON *d = cJSON_CreateObject();
        cJSON_AddStringToObject(d, "mac", mac);
        cJSON_AddNumberToObject(d, "frames", relayed[i].frames);
        cJSON_AddBoolToObject(d, "random", relayed[i].random_mac);
        cJSON_AddItemToArray(rel, d);
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
    device_t *devs    = calloc(CFG_MAX_DEVICES, sizeof(device_t));
    device_t *relayed = calloc(CFG_MAX_DEVICES, sizeof(device_t));
    assert(devs && relayed);

    for (;;) {
        vTaskDelay(pdMS_TO_TICKS(CFG_WINDOW_MS));

        size_t n, n_relayed;
        sniffer_collect(devs, &n, relayed, &n_relayed);
        csi_window_t csi;
        csi_collect(&csi);

        char *body = build_json(devs, n, relayed, n_relayed, &csi);
        if (!body) continue;
        int msg_id = mqtt_publish_batch(body);
        ESP_LOGI(TAG, "window: %u devices, %u relayed, %u csi pkts, motion=%.3f (mqtt id %d)",
                 (unsigned)n, (unsigned)n_relayed, (unsigned)csi.packets,
                 csi.motion_score, msg_id);
        cJSON_free(body);
    }
}

void batch_start(void)
{
    xTaskCreate(batch_task, "batch", 8192, NULL, 5, NULL);
}
