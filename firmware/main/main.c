#include "wifi.h"
#include "sniffer.h"
#include "csi.h"
#include "mqtt.h"
#include "batch.h"

#include "nvs_flash.h"
#include "esp_log.h"

static const char *TAG = "app";

void app_main(void)
{
    esp_err_t ret = nvs_flash_init();
    if (ret == ESP_ERR_NVS_NO_FREE_PAGES ||
        ret == ESP_ERR_NVS_NEW_VERSION_FOUND) {
        ESP_ERROR_CHECK(nvs_flash_erase());
        ret = nvs_flash_init();
    }
    ESP_ERROR_CHECK(ret);

    // 1. Associate with the home AP — pins us to its channel and gives
    //    us an IP to reach the MQTT broker.
    wifi_connect_blocking();

    // 2. MQTT client (Last Will = sensor offline detection).
    mqtt_start();

    // 3. Two independent signal sources on the same channel:
    sniffer_start();  // who: MAC + RSSI from mgmt/data frames
    csi_start();      // motion: Wi-Fi wave perturbation (CSI variance)

    // 4. Periodically batch both and publish over MQTT.
    batch_start();

    ESP_LOGI(TAG, "AirCount sensor up");
}
