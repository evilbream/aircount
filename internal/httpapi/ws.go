package httpapi

import (
	"net/http"
	"poltergeist/internal/ws"

	"github.com/coder/websocket"
)

type WSServer struct {
	hub *ws.Hub
}

func NewWSHandler(hub *ws.Hub) WSServer {
	return WSServer{
		hub: hub,
	}
}

func (s WSServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
		Subprotocols:       []string{"aircount.v1"},
	})
	if err != nil {
		http.Error(w, "failed to accept websocket connection", http.StatusInternalServerError)
		return
	}
	ctx := r.Context()

	switch c.Subprotocol() {
	case "aircount.v1":
		client := s.hub.Register(ctx, c)
		defer s.hub.Unregister(client)
	default:
		c.Close(websocket.StatusPolicyViolation, "client must speak aircount.v1")
		return
	}

	<-ctx.Done()
	c.Close(websocket.StatusCode(500), "internal server error")

}
