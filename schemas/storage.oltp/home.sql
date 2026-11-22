-- home table
CREATE SCHEMA IF NOT EXISTS home;

CREATE TABLE IF NOT EXISTS home.person (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name text NOT NULL UNIQUE,
    is_resident BOOLEAN NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);


CREATE TABLE IF NOT EXISTS home.device (
    mac macaddr PRIMARY KEY,
    person_id bigint REFERENCES home.person(id) ON DELETE SET NULL, -- NULL = unknown device
    label text,
    track_presence boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS home.device_ip (
    mac        macaddr     NOT NULL REFERENCES home.device(mac) ON DELETE CASCADE,
    ip         inet        NOT NULL,
    first_seen timestamptz NOT NULL DEFAULT now(),
    last_seen  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (mac, ip)
);
