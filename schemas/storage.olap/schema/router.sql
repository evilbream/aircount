--router status table
CREATE TABLE IF NOT EXISTS maindb.router_status (
    observed_at    DateTime64(3, 'UTC') CODEC(DoubleDelta, ZSTD(1)),
    cpu_usage      Float32 CODEC(Gorilla, ZSTD(1)), -- 0..1
    mem_usage      Float32 CODEC(Gorilla, ZSTD(1)), -- 0..1
    wan_ip         IPv4    CODEC(ZSTD(1)),
    clients_online UInt16  CODEC(T64, ZSTD(1)),
    clients_guest  UInt16  CODEC(T64, ZSTD(1)),
    wifi_24_on     Bool,
    wifi_5_on      Bool
)
ENGINE = MergeTree
PARTITION BY toYYYYMMDD(observed_at)
ORDER BY (observed_at)
TTL toDateTime(observed_at) + INTERVAL 14 DAY DELETE
SETTINGS ttl_only_drop_parts = 1;

--router clients table
CREATE TABLE IF NOT EXISTS maindb.router_clients (
    mac LowCardinality(String) CODEC(ZSTD(1)),
    ip IPv4,
    name LowCardinality(String) CODEC(ZSTD(1)),
    kind LowCardinality(String) CODEC(ZSTD(1)),
    band LowCardinality(String) CODEC(ZSTD(1)),
    guest Bool,
    signal_dbm Int8 CODEC(Delta, ZSTD(1)),
    connected_at DateTime('UTC'),
    traffic_bytes UInt64 CODEC(Delta, ZSTD(1)),
    observed_at DateTime64(3, 'UTC') CODEC(DoubleDelta, ZSTD(1))
)
ENGINE = MergeTree
PARTITION BY toYYYYMMDD(observed_at)
ORDER BY (mac, observed_at)
TTL toDateTime(observed_at) + INTERVAL 14 DAY DELETE
SETTINGS ttl_only_drop_parts = 1;
