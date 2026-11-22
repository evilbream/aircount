CREATE TABLE IF NOT EXISTS maindb.device_state (
    mac               LowCardinality(String),
    observed_at       DateTime64(3, 'UTC') CODEC(DoubleDelta, ZSTD(1)),
    online            Bool,
    presence_detected Bool,
    motion_rooms      Array(LowCardinality(String)), 
    score             Float32 CODEC(Gorilla, ZSTD(1))
) ENGINE = ReplacingMergeTree     
PARTITION BY toYYYYMM(observed_at)
ORDER BY (mac, observed_at)
TTL toDateTime(observed_at) + INTERVAL 7 DAY;