package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"poltergeist/internal/system"

	"github.com/rs/zerolog/log"
)

type Config interface {
	Load() (Config, error)
}

type Kafka struct {
	Brokers           []string // KAFKA_BROKERS, comma-separated
	GroupID           string   // KAFKA_GROUP_ID; consumers only (empty for pure producers)
	SchemaRegistryURL string   // KAFKA_SCHEMA_REGISTRY_URL; optional, for Avro/Protobuf serialization
}

func LoadKafka(groupID string) Kafka {
	return Kafka{
		Brokers:           system.EnvList("KAFKA_BROKERS", []string{"localhost:9092"}),
		GroupID:           groupID,
		SchemaRegistryURL: system.EnvDefault("KAFKA_SCHEMA_REGISTRY_URL", "http://localhost:8081"),
	}
}

func (k Kafka) Validate() error {
	if len(k.Brokers) == 0 {
		return fmt.Errorf("kafka: no brokers configured (set KAFKA_BROKERS)")
	}
	for _, b := range k.Brokers {
		if strings.TrimSpace(b) == "" {
			return fmt.Errorf("kafka: empty broker in list")
		}
	}

	if strings.TrimSpace(k.SchemaRegistryURL) == "" {
		return fmt.Errorf("kafka: schema registry url is empty (set KAFKA_SCHEMA_REGISTRY_URL)")
	}

	return nil
}

type MQTT struct {
	Broker   string // MQTT_BROKER
	ClientID string // MQTT_CLIENT_ID; must be unique per replica/instance
	Topic    string // MQTT_TOPIC;
}

func LoadMQTT() MQTT {
	clientID := system.EnvDefault("MQTT_CLIENT_ID", "ingest-gateway")
	h, err := os.Hostname()
	if err != nil || h == "" {
		h = "unknown-host"
		log.Info().Msgf("failed to get hostname: %v; using %s as client ID suffix", err, h)
	}

	// Ensure uniqueness across multiple local processes on the same host.
	clientID += "-" + h + "-" + strconv.Itoa(os.Getpid())

	return MQTT{
		Broker:   system.EnvDefault("MQTT_BROKER", "mqtt://localhost:1883"),
		ClientID: clientID,
		Topic:    system.EnvDefault("MQTT_TOPIC", "aircount/+/batch"),
	}
}

func (m MQTT) Validate() error {
	if strings.TrimSpace(m.Broker) == "" {
		return fmt.Errorf("mqtt: broker url is empty (set MQTT_BROKER)")
	}
	if strings.TrimSpace(m.ClientID) == "" {
		return fmt.Errorf("mqtt: client id is empty (set MQTT_CLIENT_ID)")
	}

	if strings.TrimSpace(m.Topic) == "" {
		return fmt.Errorf("mqtt: topic is empty (set MQTT_TOPIC)")
	}
	return nil
}

type Postgres struct {
	Host     string // POSTGRES_HOST
	Port     int    // POSTGRES_PORT
	User     string // POSTGRES_USER
	Password string // POSTGRES_PASSWORD
	Database string // POSTGRES_DB
	SSLMode  string // POSTGRES_SSLMODE: disable, require, verify-full...
	DSN      string // POSTGRES_DSN, if set, wins over the fields above
}

func LoadPostgres() Postgres {
	p := Postgres{
		Host:     system.EnvDefault("POSTGRES_HOST", "localhost"),
		Port:     system.EnvDefaultInt("POSTGRES_PORT", 5432),
		User:     system.EnvDefault("POSTGRES_USER", "admin"),
		Password: system.EnvDefault("POSTGRES_PASSWORD", "admin"),
		Database: system.EnvDefault("POSTGRES_DB", "maindb"),
		SSLMode:  system.EnvDefault("POSTGRES_SSLMODE", "disable"),
	}
	p.DSN = system.EnvDefault("POSTGRES_DSN", p.buildDSN())
	return p
}

// buildDSN escapes user and password, so special characters in them are safe.
func (p Postgres) buildDSN() string {
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(p.User, p.Password),
		Host:     net.JoinHostPort(p.Host, strconv.Itoa(p.Port)),
		Path:     "/" + p.Database,
		RawQuery: url.Values{"sslmode": {p.SSLMode}}.Encode(),
	}
	return u.String()
}

func (p Postgres) Validate() error {
	if strings.TrimSpace(p.Host) == "" {
		return fmt.Errorf("postgres: host is empty (set POSTGRES_HOST)")
	}
	if p.Port <= 0 {
		return fmt.Errorf("postgres: port is invalid (set POSTGRES_PORT)")
	}
	if strings.TrimSpace(p.User) == "" {
		return fmt.Errorf("postgres: user is empty (set POSTGRES_USER)")
	}
	if strings.TrimSpace(p.Password) == "" {
		return fmt.Errorf("postgres: password is empty (set POSTGRES_PASSWORD)")
	}
	if strings.TrimSpace(p.Database) == "" {
		return fmt.Errorf("postgres: database is empty (set POSTGRES_DB)")
	}
	if strings.TrimSpace(p.DSN) == "" {
		return fmt.Errorf("postgres: DSN is empty (set POSTGRES_DSN)")
	}
	return nil
}

type ClickHouse struct {
	Host     string
	Port     int
	User     string
	Password string
	Database string
}

func LoadClickHouse() ClickHouse {
	return ClickHouse{
		Host:     system.EnvDefault("CLICKHOUSE_HOST", "localhost"),
		Port:     system.EnvDefaultInt("CLICKHOUSE_PORT", 9000),
		User:     system.EnvDefault("CLICKHOUSE_USER", "aircount"),
		Password: system.EnvDefault("CLICKHOUSE_PASSWORD", "aircount"),
		Database: system.EnvDefault("CLICKHOUSE_DATABASE", "maindb"),
	}
}

func (c ClickHouse) Validate() error {
	if strings.TrimSpace(c.Host) == "" {
		return fmt.Errorf("clickhouse: host is empty (set CLICKHOUSE_HOST)")
	}
	if c.Port <= 0 {
		return fmt.Errorf("clickhouse: port is invalid (set CLICKHOUSE_PORT)")
	}
	if strings.TrimSpace(c.User) == "" {
		return fmt.Errorf("clickhouse: user is empty (set CLICKHOUSE_USER)")
	}
	if strings.TrimSpace(c.Password) == "" {
		return fmt.Errorf("clickhouse: password is empty (set CLICKHOUSE_PASSWORD)")
	}
	if strings.TrimSpace(c.Database) == "" {
		return fmt.Errorf("clickhouse: database is empty (set CLICKHOUSE_DATABASE)")
	}
	return nil
}

type Router struct {
	Host         string
	Username     string
	Password     string
	PollInterval time.Duration
}

func LoadRouter() Router {
	return Router{
		Host:         system.EnvDefault("ROUTER_HOST", "localhost"),
		Username:     system.EnvDefault("ROUTER_USERNAME", "admin"),
		Password:     system.EnvDefault("ROUTER_PASSWORD", "admin"),
		PollInterval: system.EnvDefaultDuration("ROUTER_POLL_INTERVAL", 30*time.Second),
	}
}

func (r Router) Validate() error {
	if strings.TrimSpace(r.Host) == "" {
		return fmt.Errorf("router: host is empty (set ROUTER_HOST)")
	}
	if strings.TrimSpace(r.Username) == "" {
		return fmt.Errorf("router: username is empty (set ROUTER_USERNAME)")
	}
	if strings.TrimSpace(r.Password) == "" {
		return fmt.Errorf("router: password is empty (set ROUTER_PASSWORD)")
	}
	if r.PollInterval <= 0 {
		return fmt.Errorf("router: poll interval is invalid (set ROUTER_POLL_INTERVAL)")
	}
	return nil
}
