package httpapi

import (
	"net/http"
	"poltergeist/internal/uiwire"
	"poltergeist/internal/ws"

	"github.com/coder/websocket"
	"github.com/rs/zerolog/log"
)

type WSServer struct {
	protocol ws.Protocol
}

func NewWsServer(protocol ws.Protocol) WSServer {
	return WSServer{
		protocol: protocol,
	}
}

func (s WSServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
		Subprotocols:       []string{string(uiwire.WsAirCountV1)},
	})
	if err != nil {
		http.Error(w, "failed to accept websocket connection", http.StatusInternalServerError)
		return
	}
	ctx := r.Context()

	if c.Subprotocol() != string(uiwire.WsAirCountV1) {
		c.Close(websocket.StatusPolicyViolation, "client must speak aircount.v1")
		return
	}

	// тут дыра по логике, елси клиент поделючился и не стал подписываться он висит и пингуется странно
	client := ws.NewClient(ctx, c)

	//send hello message on connection
	if err := s.protocol.OnConnect(ctx, client); err != nil {
		client.Close()
		return
	}
	defer func() {
		if err := s.protocol.OnDisconnect(ctx, client); err != nil {
			log.Error().Err(err).Str("session_id", client.ID()).Msg("failed to handle disconnect")
		}
	}()
	client.ReadLoop(ctx, s.protocol)

}
