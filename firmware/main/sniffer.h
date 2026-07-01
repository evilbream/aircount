#pragma once

#include <stdint.h>
#include <stddef.h>
#include <stdbool.h>

// One unique device seen during the current window.
typedef struct {
    uint8_t  mac[6];
    int8_t   rssi;        // strongest RSSI seen this window
    uint8_t  channel;
    uint16_t frames;      // how many frames from this MAC this window
    bool     random_mac;  // locally-administered bit set (MAC randomization)
} device_t;

// Enables promiscuous mode (mgmt + data) on the currently associated
// channel and starts populating the per-window device table.
void sniffer_start(void);

// Copies up to `max` collected devices into `out`, then clears the
// table for the next window. Returns the number copied.
size_t sniffer_collect(device_t *out, size_t max);
