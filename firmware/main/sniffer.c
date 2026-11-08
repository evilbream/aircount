#include "sniffer.h"
#include "app_config.h"

#include <string.h>
#include <stdlib.h>
#include <assert.h>

#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "esp_wifi.h"
#include "esp_event.h"
#include "esp_log.h"

static const char *TAG = "sniffer";

// First 24 bytes of an 802.11 header, the same for mgmt and data frames:
//
//    0  frame control  byte 0: type/subtype, byte 1: flags (ToDS, FromDS…)
//    2  duration
//    4  addr1          receiver
//   10  addr2          transmitter — the radio that actually sent it
//   16  addr3          mgmt: BSSID
//                      data ToDS:   final destination
//                      data FromDS: original source (SA)
//   22  sequence control
//
// QoS data frames add fields after byte 24, so these offsets hold for
// them too.
#define ADDR2_OFFSET 10
#define ADDR3_OFFSET 16
#define HDR_LEN      24

#define FC_TO_DS   0x01
#define FC_FROM_DS 0x02

// Frame control byte 0 with the protocol version bits masked off:
// type 0 (mgmt), subtype 4.
#define FC0_PROBE_REQ 0x40

#define FCS_LEN 4  // rx_ctrl.sig_len includes the 802.11 FCS

// Information element IDs whose content is left out of the fingerprint.
#define IE_SSID         0    // differs between wildcard and directed probes
#define IE_DS_PARAMS    3    // the channel the probe was sent on
#define IE_OP_CLASSES   59   // byte 0 is the current operating class
#define IE_VENDOR       221  // payload varies; OUI + type is stable

#define MAC_FMT "%02x:%02x:%02x:%02x:%02x:%02x"
#define MAC_ARG(m) (m)[0], (m)[1], (m)[2], (m)[3], (m)[4], (m)[5]

typedef struct {
    device_t *entries;  // CFG_MAX_DEVICES
    size_t    count;
} table_t;

static table_t s_devices;   // radios heard directly
static table_t s_relayed;   // sources of frames our AP forwarded
static uint8_t s_bssid[6];  // AP we are associated with
static uint8_t s_own_mac[6];
static SemaphoreHandle_t s_lock;

static bool is_group(const uint8_t *mac) { return mac[0] & 0x01; }

// Routers (this TP-Link included) derive all their MACs — the BSSID of
// every band and SSID, LAN, WAN — from one base address. They differ in
// the last byte and sometimes in the first, so equal bytes 1..4 mean
// "the router itself". Without this, the router's LAN MAC (the source
// of ping replies to every sensor and of its ARP requests) would top
// the relayed list.
static bool is_our_router(const uint8_t *mac)
{
    return memcmp(mac + 1, s_bssid + 1, 4) == 0;
}

// FNV-1a over the probe request's information elements: their order,
// IDs and contents (supported rates, HT/VHT/HE capabilities, extended
// capabilities, …). These describe the chipset and OS build, not the
// MAC, so they stay the same when the phone randomizes its address.
// Fields that change from probe to probe are left out (see IE_* above).
static uint32_t probe_fingerprint(const uint8_t *frame, int len)
{
    uint32_t h = 2166136261u;
    int i = HDR_LEN;  // probe requests have no fixed fields: IEs start here
    while (i + 2 <= len) {
        uint8_t id = frame[i];
        uint8_t n  = frame[i + 1];
        if (i + 2 + n > len) break;  // truncated element

        const uint8_t *v = frame + i + 2;
        int skip = 0, take = n;
        switch (id) {
        case IE_SSID:
        case IE_DS_PARAMS:  take = 0;                 break;
        case IE_OP_CLASSES: skip = n ? 1 : 0;         break;
        case IE_VENDOR:     take = n < 4 ? n : 4;     break;
        }

        h = (h ^ id) * 16777619u;
        for (int k = skip; k < take; k++) h = (h ^ v[k]) * 16777619u;
        i += 2 + n;
    }
    return h ? h : 1;  // 0 means "no fingerprint"
}

static void table_add(table_t *t, const uint8_t *mac,
                      int8_t rssi, uint8_t channel, uint32_t fp)
{
    for (size_t i = 0; i < t->count; i++) {
        device_t *d = &t->entries[i];
        if (memcmp(d->mac, mac, 6) == 0) {
            if (d->frames < UINT16_MAX) d->frames++;
            if (rssi > d->rssi) d->rssi = rssi;
            d->channel = channel;
            if (fp) d->fp = fp;
            return;
        }
    }
    if (t->count >= (size_t)CFG_MAX_DEVICES) return;

    device_t *d = &t->entries[t->count++];
    memcpy(d->mac, mac, 6);
    d->rssi       = rssi;
    d->channel    = channel;
    d->frames     = 1;
    d->random_mac = (mac[0] & 0x02) != 0;  // locally administered
    d->fp         = fp;
}

static void on_packet(void *buf, wifi_promiscuous_pkt_type_t type)
{
    if (type != WIFI_PKT_MGMT && type != WIFI_PKT_DATA) return;

    const wifi_promiscuous_pkt_t *pkt = (const wifi_promiscuous_pkt_t *)buf;
    if (pkt->rx_ctrl.sig_len < HDR_LEN) return;

    const uint8_t *hdr = pkt->payload;
    const uint8_t *a2  = hdr + ADDR2_OFFSET;
    const uint8_t *a3  = hdr + ADDR3_OFFSET;

    xSemaphoreTake(s_lock, portMAX_DELAY);

    table_t       *table = NULL;
    const uint8_t *mac   = NULL;
    uint32_t       fp    = 0;

    if (type == WIFI_PKT_MGMT) {
        // addr3 is the BSSID. Transmitter == BSSID means an AP sent it:
        // beacons, probe responses, the AP's own deauth/action frames.
        // A phone's probe request carries a wildcard (ff:ff:…) or some
        // AP's BSSID in addr3, never its own MAC, so it still passes.
        if (memcmp(a2, a3, 6) != 0) {
            table = &s_devices;
            mac   = a2;
            if ((hdr[0] & 0xfc) == FC0_PROBE_REQ) {
                fp = probe_fingerprint(hdr, pkt->rx_ctrl.sig_len - FCS_LEN);
            }
        }
    } else {
        switch (hdr[1] & (FC_TO_DS | FC_FROM_DS)) {
        case FC_TO_DS:  // station → AP: addr2 is the station
        case 0:         // station ↔ station (IBSS, TDLS)
            table = &s_devices;
            mac   = a2;
            break;

        case FC_FROM_DS:
            // AP → its stations: addr2 is the AP, so it is not a device.
            // addr3 is whoever originally sent the frame: a 5 GHz client
            // or wired host when our router bridges its broadcast here.
            // Neighbours' APs are skipped: their hosts are not ours.
            if (memcmp(a2, s_bssid, 6) == 0 &&
                memcmp(a3, s_own_mac, 6) != 0 &&  // our own broadcast echoed
                !is_our_router(a3)) {
                table = &s_relayed;
                mac   = a3;
            }
            break;

        default:        // ToDS + FromDS: WDS/mesh link between two APs
            break;
        }
    }

    if (table && !is_group(mac)) {
        table_add(table, mac, pkt->rx_ctrl.rssi, pkt->rx_ctrl.channel, fp);
    }

    xSemaphoreGive(s_lock);
}

// Keeps the relay filter on the right AP if we reconnect to another
// one with the same SSID (mesh, band steering).
static void on_connected(void *arg, esp_event_base_t base,
                         int32_t id, void *data)
{
    const wifi_event_sta_connected_t *e = data;
    xSemaphoreTake(s_lock, portMAX_DELAY);
    memcpy(s_bssid, e->bssid, 6);
    xSemaphoreGive(s_lock);
    ESP_LOGI(TAG, "relay source is now AP " MAC_FMT, MAC_ARG(e->bssid));
}

void sniffer_start(void)
{
    s_devices.entries = calloc(CFG_MAX_DEVICES, sizeof(device_t));
    s_relayed.entries = calloc(CFG_MAX_DEVICES, sizeof(device_t));
    assert(s_devices.entries && s_relayed.entries);
    s_lock = xSemaphoreCreateMutex();
    assert(s_lock);

    ESP_ERROR_CHECK(esp_wifi_get_mac(WIFI_IF_STA, s_own_mac));
    wifi_ap_record_t ap;
    if (esp_wifi_sta_get_ap_info(&ap) == ESP_OK) {
        memcpy(s_bssid, ap.bssid, 6);
    } else {
        ESP_LOGW(TAG, "no AP info yet, relayed list stays empty until connect");
    }
    ESP_ERROR_CHECK(esp_event_handler_instance_register(
        WIFI_EVENT, WIFI_EVENT_STA_CONNECTED, &on_connected, NULL, NULL));

    wifi_promiscuous_filter_t filter = {
        .filter_mask = WIFI_PROMIS_FILTER_MASK_MGMT |
                       WIFI_PROMIS_FILTER_MASK_DATA,
    };
    ESP_ERROR_CHECK(esp_wifi_set_promiscuous_filter(&filter));
    ESP_ERROR_CHECK(esp_wifi_set_promiscuous_rx_cb(&on_packet));
    ESP_ERROR_CHECK(esp_wifi_set_promiscuous(true));
    ESP_LOGI(TAG, "promiscuous sniffer started (mgmt + data), APs filtered, "
             "relay source AP " MAC_FMT, MAC_ARG(s_bssid));
}

static size_t drain(table_t *t, device_t *out)
{
    size_t n = t->count;
    memcpy(out, t->entries, n * sizeof(device_t));
    t->count = 0;
    return n;
}

void sniffer_collect(device_t *devices, size_t *n_devices,
                     device_t *relayed, size_t *n_relayed)
{
    xSemaphoreTake(s_lock, portMAX_DELAY);
    *n_devices = drain(&s_devices, devices);
    *n_relayed = drain(&s_relayed, relayed);
    xSemaphoreGive(s_lock);
}
