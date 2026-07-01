#pragma once

// Starts the background task that, once per CFG_WINDOW_MS, drains the
// sniffer + CSI accumulators, builds a JSON batch and POSTs it to the
// ingest-gateway.
void batch_start(void);
