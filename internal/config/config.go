package config

import (
	"fmt"
	"os"
	"strings"

	"poltergeist/internal/system"

	"github.com/rs/zerolog/log"
)

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

	clientID += "-" + h

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
	DSN string // POSTGRES_DSN, e.g. postgres://user:pass@host:5432/db
}

func LoadPostgres() Postgres {
	return Postgres{DSN: system.EnvDefault("POSTGRES_DSN", "")}
}

func (p Postgres) Validate() error {
	if strings.TrimSpace(p.DSN) == "" {
		return fmt.Errorf("postgres: DSN is empty (set POSTGRES_DSN)")
	}
	return nil
}
