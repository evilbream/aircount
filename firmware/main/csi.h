#pragma once

#include <stdint.h>

// Aggregated CSI result for one window.
typedef struct {
    uint32_t packets;       // CSI frames seen this window
    double   motion_score;  // variance of per-frame mean amplitude
    int      subcarriers;   // subcarriers in the last frame (info only)
} csi_window_t;

// Registers the CSI receive callback and enables capture. CSI frames
// arrive from the associated AP's traffic (beacons are enough), so no
// extra packet generation is required for a first version.
void csi_start(void);

// Snapshots the current window's CSI stats into `out` and resets the
// accumulator for the next window.
void csi_collect(csi_window_t *out);
