#include "csi.h"

#include <math.h>
#include <string.h>

#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "esp_wifi.h"
#include "esp_netif.h"
#include "esp_log.h"
#include "ping/ping_sock.h"
#include "lwip/ip_addr.h"

static const char *TAG = "csi";

// Max subcarriers we track (ESP32-S3 reports up to 128 for HT40).
#define MAX_SUBC 128

// Per-subcarrier accumulators over the window. We keep amplitude stats
// for EACH subcarrier (not a single averaged value): motion perturbs
// subcarriers differently, and averaging them together cancels the
// signal out. motion_score = mean of per-subcarrier variance.
static struct {
    double   sum[MAX_SUBC];    // Σ normalized amplitude
    double   sqsum[MAX_SUBC];  // Σ normalized amplitude^2
    uint32_t cnt[MAX_SUBC];    // samples per subcarrier
    uint32_t packets;          // CSI frames accepted this window
    int      last_n;           // subcarriers in the last frame (info)
} s_acc;

static SemaphoreHandle_t s_lock;

// Only accept CSI from the associated AP — a fixed source at a fixed
// distance gives a stable baseline, so variance reflects motion rather
// than "whoever happened to transmit".
static uint8_t s_ap_bssid[6];
static bool    s_filter_ap;

// Scratch, only touched inside the (serialized) CSI callback.
static double s_amp[MAX_SUBC];

static void on_csi(void *ctx, wifi_csi_info_t *info)
{
    if (!info || !info->buf || info->len < 2) return;
    if (s_filter_ap && memcmp(info->mac, s_ap_bssid, 6) != 0) return;

    const int8_t *buf = info->buf;   // interleaved [imag, real] per subcarrier
    int n = info->len / 2;
    if (n > MAX_SUBC) n = MAX_SUBC;
    // First 4 bytes (2 subcarriers) can be invalid due to a HW quirk.
    int start = info->first_word_invalid ? 2 : 0;
    if (n - start < 4) return;

    // Per-subcarrier amplitude + this frame's mean (for normalization).
    double mean = 0.0;
    int m = 0;
    for (int i = start; i < n; i++) {
        int im = buf[2 * i];
        int re = buf[2 * i + 1];
        double a = sqrt((double)(re * re + im * im));
        s_amp[i] = a;
        mean += a;
        m++;
    }
    if (m == 0 || mean <= 0.0) return;
    mean /= m;

    xSemaphoreTake(s_lock, portMAX_DELAY);
    for (int i = start; i < n; i++) {
        // Normalize by the frame mean → cancels AGC / overall gain
        // jumps, leaving only the *shape* change that motion causes.
        double na = s_amp[i] / mean;
        s_acc.sum[i]   += na;
        s_acc.sqsum[i] += na * na;
        s_acc.cnt[i]++;
    }
    s_acc.packets++;
    s_acc.last_n = n;
    xSemaphoreGive(s_lock);
}

// Generates a steady stream of RX frames from the AP by pinging the
// gateway, so CSI arrives dozens of times per window instead of relying
// on the sparse ~10 Hz beacon rate.
static void start_ping_traffic(void)
{
    esp_netif_t *sta = esp_netif_get_handle_from_ifkey("WIFI_STA_DEF");
    esp_netif_ip_info_t ip;
    if (!sta || esp_netif_get_ip_info(sta, &ip) != ESP_OK || ip.gw.addr == 0) {
        ESP_LOGW(TAG, "no gateway found, skipping CSI ping traffic");
        return;
    }

    ip_addr_t target;
    memset(&target, 0, sizeof(target));
    ip_addr_set_ip4_u32(&target, ip.gw.addr);

    esp_ping_config_t cfg = ESP_PING_DEFAULT_CONFIG();
    cfg.target_addr = target;
    cfg.interval_ms = 50;   // ~20 pings/s → steady CSI feed
    cfg.count = 0;          // run forever

    esp_ping_callbacks_t cbs = { 0 };  // we don't care about replies, only RX
    esp_ping_handle_t ping;
    if (esp_ping_new_session(&cfg, &cbs, &ping) == ESP_OK) {
        esp_ping_start(ping);
        ESP_LOGI(TAG, "CSI ping traffic started (gateway, 50ms)");
    } else {
        ESP_LOGW(TAG, "failed to start CSI ping session");
    }
}

void csi_start(void)
{
    s_lock = xSemaphoreCreateMutex();

    // Lock the CSI filter onto the AP we associated with.
    wifi_ap_record_t ap;
    if (esp_wifi_sta_get_ap_info(&ap) == ESP_OK) {
        memcpy(s_ap_bssid, ap.bssid, 6);
        s_filter_ap = true;
        ESP_LOGI(TAG, "CSI locked to AP %02x:%02x:%02x:%02x:%02x:%02x",
                 ap.bssid[0], ap.bssid[1], ap.bssid[2],
                 ap.bssid[3], ap.bssid[4], ap.bssid[5]);
    } else {
        s_filter_ap = false;
        ESP_LOGW(TAG, "no AP info, CSI source filter disabled");
    }

    wifi_csi_config_t cfg = {
        .lltf_en           = true,
        .htltf_en          = true,
        .stbc_htltf2_en    = true,
        .ltf_merge_en      = true,
        .channel_filter_en = true,
        .manu_scale        = false,
        .shift             = 0,
    };
    ESP_ERROR_CHECK(esp_wifi_set_csi_config(&cfg));
    ESP_ERROR_CHECK(esp_wifi_set_csi_rx_cb(&on_csi, NULL));
    ESP_ERROR_CHECK(esp_wifi_set_csi(true));

    start_ping_traffic();
    ESP_LOGI(TAG, "CSI capture started (per-subcarrier, AP-filtered)");
}

void csi_collect(csi_window_t *out)
{
    xSemaphoreTake(s_lock, portMAX_DELAY);
    out->packets     = s_acc.packets;
    out->subcarriers = s_acc.last_n;

    // motion_score = average of per-subcarrier variance over the window.
    double var_sum = 0.0;
    int used = 0;
    for (int i = 0; i < MAX_SUBC; i++) {
        if (s_acc.cnt[i] > 1) {
            double mean = s_acc.sum[i] / s_acc.cnt[i];
            double var  = s_acc.sqsum[i] / s_acc.cnt[i] - mean * mean;
            if (var > 0.0) { var_sum += var; used++; }
        }
    }
    out->motion_score = used ? (var_sum / used) : 0.0;

    memset(s_acc.sum,   0, sizeof(s_acc.sum));
    memset(s_acc.sqsum, 0, sizeof(s_acc.sqsum));
    memset(s_acc.cnt,   0, sizeof(s_acc.cnt));
    s_acc.packets = 0;
    xSemaphoreGive(s_lock);
}
