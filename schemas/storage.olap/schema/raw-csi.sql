-- raw CSI data
CREATE TABLE IF NOT EXISTS maindb.csi_windows (
    observed_at  DateTime64(3, 'UTC') CODEC(DoubleDelta, ZSTD(1)),
    sensor_id    LowCardinality(String),
    motion_score Float64 CODEC(Gorilla, ZSTD(1)),
    packets      UInt32  CODEC(T64, ZSTD(1)),
    subcarriers  UInt16  CODEC(T64, ZSTD(1)),
    window_ms    UInt32  CODEC(T64, ZSTD(1)),
    uptime_ms    UInt64  CODEC(Delta, ZSTD(1))
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(observed_at)
ORDER BY (sensor_id, observed_at)
TTL toDateTime(observed_at) + INTERVAL 1 YEAR DELETE
SETTINGS ttl_only_drop_parts = 1;
