package httpapi

import (
	"context"
	"net/http"
	"poltergeist/internal/domain"
	"poltergeist/internal/uiwire"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

type csiHistoryReader interface {
	CSIWindows(ctx context.Context, q domain.HistoryQuery) ([]domain.CSIWindow, error)
}

type SimpleWS struct {
	history csiHistoryReader
}

func NewSimpleWS(h csiHistoryReader) *SimpleWS {
	return &SimpleWS{
		history: h,
	}
}

func (h *SimpleWS) healthCheck(w http.ResponseWriter, r *http.Request) {
	writeJSON(r.Context(), w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *SimpleWS) listSensors(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(r.Context(), w, http.StatusMethodNotAllowed, responseError{
			Code:    http.StatusMethodNotAllowed,
			Message: "method not allowed",
		})
		return
	}

	w.Header().Set("Access-Control-Allow-Origin", "*")

	sensors := []string{"living-room", "bedroom", "kitchen"}
	writeJSON(r.Context(), w, http.StatusOK, sensors)

}

func (h *SimpleWS) csHistory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if r.Method != http.MethodGet {
		writeJSON(r.Context(), w, http.StatusMethodNotAllowed, responseError{
			Code:    http.StatusMethodNotAllowed,
			Message: "method not allowed",
		})
		return
	}

	w.Header().Set("Access-Control-Allow-Origin", "*")
	// TODO add user wrong data paass protection
	sensors := make([]string, 0)
	sensorsStr := r.URL.Query().Get("sensor")
	if sensorsStr != "" {
		sensors = strings.Split(sensorsStr, ",")
	} else { // remove hardcode
		sensors = []string{"living-room", "bedroom", "kitchen"}
	}

	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")

	from, err := strconv.Atoi(fromStr)

	if err != nil {
		log.Info().Err(err).Msg("incorrect from parameter")
		writeJSON(r.Context(), w, http.StatusBadRequest, responseError{
			Code:    http.StatusBadRequest,
			Message: "missing or incorrect 'from' parameter",
		})
		return
	}

	to := int(time.Now().UnixMilli())
	if toStr != "" {
		if to, err = strconv.Atoi(toStr); err != nil {
			writeJSON(ctx, w, http.StatusBadRequest, responseError{Code: 400, Message: "to must be unix ms"})
			return
		}
	}
	q, err := domain.NewHistoryQuery(sensors, time.UnixMilli(int64(from)), time.UnixMilli(int64(to)))
	if err != nil {
		writeJSON(ctx, w, http.StatusBadRequest, responseError{Code: 400, Message: "invalid history query"})
		return
	}
	windows, err := h.history.CSIWindows(ctx, q)
	if err != nil {
		log.Info().Err(err).Msg("failed to fetch history")
		writeJSON(ctx, w, http.StatusInternalServerError, responseError{Code: 500, Message: "failed to fetch history"})
		return
	}
	writeJSON(ctx, w, http.StatusOK, uiwire.NewHistory(q, windows))
}
