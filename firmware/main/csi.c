#include "csi.h"

#include <math.h>

#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "esp_wifi.h"
#include "esp_log.h"

static const char *TAG = "csi";

// Running accumulator over per-frame mean amplitudes. Motion is
// estimated as the variance of that mean across the window: a still
// room gives near-constant amplitude (low variance), a moving body
// perturbs the multipath and raises it.
static struct {
    uint32_t packets;
    double   sum;     // Σ mean_amp
    double   sqsum;   // Σ mean_amp^2
    int      subc;
} s_acc;

static SemaphoreHandle_t s_lock;

static void on_csi(void *ctx, wifi_csi_info_t *info)
{
    if (!info || !info->buf || info->len < 2) return;

    const int8_t *buf = info->buf;   // interleaved [imag, real] per subcarrier
    int n = info->len / 2;

    double sum_amp = 0.0;
    for (int i = 0; i < n; i++) {
        int im = buf[2 * i];
        int re = buf[2 * i + 1];
        sum_amp += sqrt((double)(re * re + im * im));
    }
    double mean_amp = sum_amp / n;

    xSemaphoreTake(s_lock, portMAX_DELAY);
    s_acc.packets++;
    s_acc.sum   += mean_amp;
    s_acc.sqsum += mean_amp * mean_amp;
    s_acc.subc   = n;
    xSemaphoreGive(s_lock);
}

void csi_start(void)
{
    s_lock = xSemaphoreCreateMutex();

    // Capture both legacy and HT long training fields; filter to the
    // associated channel so amplitudes are comparable frame-to-frame.
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
    ESP_LOGI(TAG, "CSI capture started");
}

void csi_collect(csi_window_t *out)
{
    xSemaphoreTake(s_lock, portMAX_DELAY);
    out->packets     = s_acc.packets;
    out->subcarriers = s_acc.subc;
    if (s_acc.packets > 1) {
        double mean = s_acc.sum / s_acc.packets;
        double var  = s_acc.sqsum / s_acc.packets - mean * mean;
        out->motion_score = var > 0.0 ? var : 0.0;
    } else {
        out->motion_score = 0.0;
    }
    s_acc.packets = 0;
    s_acc.sum     = 0.0;
    s_acc.sqsum   = 0.0;
    xSemaphoreGive(s_lock);
}
