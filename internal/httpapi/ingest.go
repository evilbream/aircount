package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"poltergeist/internal/sensorwire"

	"github.com/rs/zerolog"
)

type ingestService interface {
	Ingest(ctx context.Context, batch sensorwire.Batch) error
}

type Ingesthandler struct {
	svc ingestService
}

func NewIngestHandler(svc ingestService) *Ingesthandler {
	return &Ingesthandler{svc: svc}

}

func (h *Ingesthandler) healthCheck(w http.ResponseWriter, r *http.Request) {
	writeJSON(r.Context(), w, http.StatusOK, map[string]string{"status": "ok"})

}

func (h *Ingesthandler) ingest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var batch sensorwire.Batch
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
		zerolog.Ctx(ctx).Error().Err(err).Msg("failed to decode request body")
		writeJSON(ctx, w, http.StatusBadRequest, responseError{
			Code:    http.StatusBadRequest,
			Message: "invalid request body",
			Error:   err,
		})
		return
	}
	_, _ = io.Copy(io.Discard, r.Body)

	if err := batch.Validate(); err != nil {
		zerolog.Ctx(ctx).Error().Err(err).Msg("invalid batch data")
		writeJSON(ctx, w, http.StatusBadRequest, responseError{
			Code:    http.StatusBadRequest,
			Message: "invalid batch data",
			Error:   err,
		})
		return
	}

	if err := h.svc.Ingest(ctx, batch); err != nil {
		zerolog.Ctx(ctx).Error().Err(err).Msg("failed to ingest data")
		writeJSON(ctx, w, http.StatusInternalServerError, responseError{
			Code:    http.StatusInternalServerError,
			Message: "failed to ingest data",
		})
		return
	}

	writeJSON(ctx, w, http.StatusOK, map[string]string{"status": "ok"})
}
