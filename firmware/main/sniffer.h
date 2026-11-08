#pragma once

#include <stdint.h>
#include <stddef.h>
#include <stdbool.h>

// One unique MAC seen during the current window.
typedef struct {
    uint8_t  mac[6];
    int8_t   rssi;        // strongest RSSI seen this window
    uint8_t  channel;
    uint16_t frames;      // how many frames counted for this MAC this window
    bool     random_mac;  // locally-administered bit set (MAC randomization)
    uint32_t fp;          // probe request fingerprint, 0 = no probe this window
} device_t;

// Enables promiscuous mode (mgmt + data) on the associated channel and
// fills two per-window tables:
//
//   devices — radios this board heard transmitting (addr2). Access
//             points are left out (beacons, probe responses, the AP's
//             side of data traffic). RSSI is the device's own signal.
//             For probe requests `fp` hashes the capabilities the phone
//             advertises: it survives MAC randomization, so throwaway
//             MACs with the same fp most likely belong to one device
//             (or to identical phone models on the same OS version).
//
//   relayed — hosts whose frames our AP forwarded into the 2.4 GHz BSS
//             (addr3 of FromDS frames sent by our BSSID). This is how
//             clients on the 5 GHz SSID, and wired hosts, show up on a
//             2.4 GHz radio: the router repeats their broadcast/multicast
//             (mDNS, ARP, SSDP…) on every band. The radio signal is the
//             AP's, so rssi/channel mean nothing for these entries.
//
// Call once, after Wi-Fi is associated.
void sniffer_start(void);

// Copies the current window into `devices` and `relayed` (each must
// hold CFG_MAX_DEVICES entries), stores the counts and clears both
// tables for the next window.
void sniffer_collect(device_t *devices, size_t *n_devices,
                     device_t *relayed, size_t *n_relayed);
