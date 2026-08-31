package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/rs/zerolog"
)

type responseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Error   error  `json:"error,omitempty"`
}

func healthCheck(w http.ResponseWriter, r *http.Request) {
	writeJSON(r.Context(), w, http.StatusOK, map[string]string{"status": "ok"})

}

func writeJSON(ctx context.Context, w http.ResponseWriter, status int, body any) {
	var buf []byte
	if body != nil {
		var err error
		buf, err = json.Marshal(body)
		if err != nil {
			zerolog.Ctx(ctx).Error().Err(err).Msg("failed to marshal JSON response")
			http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if buf != nil {
		if _, err := w.Write(buf); err != nil {
			zerolog.Ctx(ctx).Error().Err(err).Msg("failed to write JSON response")
		}
	}
}
