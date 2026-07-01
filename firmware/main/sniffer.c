#include "sniffer.h"
#include "app_config.h"

#include <string.h>
#include <stdlib.h>
#include <assert.h>

#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "esp_wifi.h"
#include "esp_log.h"

static const char *TAG = "sniffer";

// 802.11 MAC header: addr2 (transmitter) starts at byte 10. For frames
// a device sends (probe requests, data to the AP) this is the device's
// own MAC — exactly what we want to count.
#define ADDR2_OFFSET 10
#define MIN_FRAME_LEN 16

static device_t      *s_table;       // CFG_MAX_DEVICES entries
static size_t         s_count;
static SemaphoreHandle_t s_lock;

static int find_locked(const uint8_t *mac)
{
    for (size_t i = 0; i < s_count; i++) {
        if (memcmp(s_table[i].mac, mac, 6) == 0) return (int)i;
    }
    return -1;
}

static void on_packet(void *buf, wifi_promiscuous_pkt_type_t type)
{
    if (type != WIFI_PKT_MGMT && type != WIFI_PKT_DATA) return;

    const wifi_promiscuous_pkt_t *pkt = (const wifi_promiscuous_pkt_t *)buf;
    if (pkt->rx_ctrl.sig_len < MIN_FRAME_LEN) return;

    const uint8_t *src = pkt->payload + ADDR2_OFFSET;
    // Skip broadcast/multicast as a source (shouldn't happen for addr2,
    // but guards against malformed frames).
    if (src[0] & 0x01 && memcmp(src, "\xff\xff\xff\xff\xff\xff", 6) == 0) return;

    int8_t  rssi    = pkt->rx_ctrl.rssi;
    uint8_t channel = pkt->rx_ctrl.channel;

    xSemaphoreTake(s_lock, portMAX_DELAY);
    int idx = find_locked(src);
    if (idx >= 0) {
        s_table[idx].frames++;
        if (rssi > s_table[idx].rssi) s_table[idx].rssi = rssi;
        s_table[idx].channel = channel;
    } else if (s_count < (size_t)CFG_MAX_DEVICES) {
        device_t *d = &s_table[s_count++];
        memcpy(d->mac, src, 6);
        d->rssi       = rssi;
        d->channel    = channel;
        d->frames     = 1;
        d->random_mac = (src[0] & 0x02) != 0;  // locally administered
    }
    xSemaphoreGive(s_lock);
}

void sniffer_start(void)
{
    s_table = calloc(CFG_MAX_DEVICES, sizeof(device_t));
    assert(s_table);
    s_lock = xSemaphoreCreateMutex();

    wifi_promiscuous_filter_t filter = {
        .filter_mask = WIFI_PROMIS_FILTER_MASK_MGMT |
                       WIFI_PROMIS_FILTER_MASK_DATA,
    };
    ESP_ERROR_CHECK(esp_wifi_set_promiscuous_filter(&filter));
    ESP_ERROR_CHECK(esp_wifi_set_promiscuous_rx_cb(&on_packet));
    ESP_ERROR_CHECK(esp_wifi_set_promiscuous(true));
    ESP_LOGI(TAG, "promiscuous sniffer started (mgmt + data)");
}

size_t sniffer_collect(device_t *out, size_t max)
{
    xSemaphoreTake(s_lock, portMAX_DELAY);
    size_t n = s_count < max ? s_count : max;
    memcpy(out, s_table, n * sizeof(device_t));
    s_count = 0;  // reset window
    xSemaphoreGive(s_lock);
    return n;
}
