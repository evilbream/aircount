package httpapi

import (
	"context"
	"net/http"
	"poltergeist/internal/domain"
	"strconv"
)

type presenceService interface {
	ListLastPresence(ctx context.Context, sensor_id string, limit int) ([]domain.PresenceRF, error)
}

type PresenceHandler struct {
	svc presenceService
}

func NewPresenceHandler(ps presenceService) *PresenceHandler {
	return &PresenceHandler{svc: ps}
}

func (h *PresenceHandler) listLastPresence(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sensorID := r.URL.Query().Get("sensor_id")
	limitStr := r.URL.Query().Get("limit")

	if sensorID == "" {
		writeJSON(ctx, w, http.StatusBadRequest, responseError{
			Code:    http.StatusBadRequest,
			Message: "sensor_id is required",
		})
		return
	}

	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		writeJSON(ctx, w, http.StatusBadRequest, responseError{
			Code:    http.StatusBadRequest,
			Message: "limit must be a positive integer",
		})
		return
	}

	presences, err := h.svc.ListLastPresence(ctx, sensorID, limit)
	if err != nil {
		writeJSON(ctx, w, http.StatusInternalServerError, responseError{
			Code:    http.StatusInternalServerError,
			Message: "failed to retrieve presence data",
			Error:   err,
		})
		return
	}

	writeJSON(ctx, w, http.StatusOK, presences)

}
