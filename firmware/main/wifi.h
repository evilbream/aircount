#pragma once

// Connects to the home AP in STA mode and blocks until an IP is
// acquired. Associating pins the radio to the AP's channel, which is
// what lets the promiscuous sniffer and CSI capture coexist.
void wifi_connect_blocking(void);
