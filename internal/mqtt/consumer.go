package mqtt

import (
	"context"
	"net/url"
	"poltergeist/internal/config"
	"poltergeist/internal/sensorwire"

	"github.com/eclipse/paho.golang/autopaho"
	"github.com/eclipse/paho.golang/paho"
	"github.com/rs/zerolog/log"
)

type ingestService interface {
	Ingest(ctx context.Context, batch sensorwire.Batch) error
}

type Consumer struct {
	cfg config.MQTT
	svc ingestService
}

func NewConsumer(cfg config.MQTT, svc ingestService) *Consumer {
	return &Consumer{
		cfg: cfg,
		svc: svc,
	}
}

func (c *Consumer) onConnection(cm *autopaho.ConnectionManager, connAck *paho.Connack) {
	log.Info().Msg("mqtt connection established")
	if _, err := cm.Subscribe(context.Background(), &paho.Subscribe{
		Subscriptions: []paho.SubscribeOptions{
			{Topic: c.cfg.Topic, QoS: 1},
		},
	}); err != nil {
		log.Fatal().Err(err).Msg("failed to subscribe to topic")
	}
	log.Info().Msgf("subscribed to topic %s", c.cfg.Topic)
}

func (c *Consumer) onConnectionError(err error) {
	log.Error().Err(err).Msg("failed to connect to mqtt broker")
}

func (c *Consumer) onCLientError(err error) {
	log.Error().Err(err).Msg("client error")
}

func (c *Consumer) onMessageReceived(ctx context.Context, pr paho.PublishReceived) (bool, error) {
	log.Debug().Msgf("received message on topic %s: %v", pr.Packet.Topic, string(pr.Packet.Payload))
	batch, err := sensorwire.Decode(pr.Packet.Payload)
	if err != nil {
		log.Error().Err(err).Msgf("failed to decode payload: %s", string(pr.Packet.Payload))
		// TODO: опубликовать в DLQ
		return true, err
	}
	if err := c.svc.Ingest(ctx, batch); err != nil {
		log.Error().Err(err).Msg("failed to ingest data")
		return false, err
	}
	return true, nil
}

func (c *Consumer) onCLientDisconnect(d *paho.Disconnect) {
	if d.Properties != nil {
		log.Info().Msgf("client disconnected: %d, reason: %s", d.ReasonCode, d.Properties.ReasonString)
		return
	}
	log.Info().Msgf("client disconnected: %d", d.ReasonCode)
}

func (c *Consumer) Start(ctx context.Context) error {
	log.Debug().Msgf("starting MQTT consumer with config: %+v", c.cfg)
	u, err := url.Parse(c.cfg.Broker)
	if err != nil {
		return err
	}
	cliCfg := autopaho.ClientConfig{
		ServerUrls:                    []*url.URL{u},
		KeepAlive:                     20,
		CleanStartOnInitialConnection: false,
		SessionExpiryInterval:         60,
		OnConnectionUp:                c.onConnection,
		OnConnectError:                c.onConnectionError,
		ClientConfig: paho.ClientConfig{
			ClientID:           c.cfg.ClientID,
			OnPublishReceived:  []func(paho.PublishReceived) (bool, error){func(pr paho.PublishReceived) (bool, error) { return c.onMessageReceived(ctx, pr) }},
			OnClientError:      c.onCLientError,
			OnServerDisconnect: c.onCLientDisconnect,
		},
	}
	client, err := autopaho.NewConnection(ctx, cliCfg)
	if err != nil {
		return err
	}

	if err := client.AwaitConnection(ctx); err != nil {
		return err
	}
	<-client.Done()
	client.Disconnect(ctx)
	log.Debug().Msg("MQTT consumer stopped")
	return nil
}
