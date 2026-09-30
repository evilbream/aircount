package httpapi

import (
	"net/http"
	"runtime/debug"
	"time"
	"uuid"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const requestIDHeader = "X-Request-ID"

func NewIngestRouter(is ingestService) http.Handler {
	mux := http.NewServeMux()

	h := NewIngestHandler(is)

	mux.HandleFunc("GET /healtz", h.healthCheck)
	mux.HandleFunc("POST /ingest", h.ingest)
	return withMiddleware(mux)

}

func NewPresenceRouter(ps presenceService) http.Handler {
	mux := http.NewServeMux()

	h := NewPresenceHandler(ps)

	mux.HandleFunc("GET /healtz", healthCheck)
	mux.HandleFunc("POST /listLastPresence", h.listLastPresence)
	return withMiddleware(mux)

}

func withMiddleware(next http.Handler) http.Handler {
	return requestID(recoverer(logging(next)))
}

func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {

			rec := recover()
			if rec == nil {
				return
			}

			if rec == http.ErrAbortHandler {
				log.Warn().Msg("request aborted")
				panic(rec)
			}

			ctx := r.Context()

			zerolog.Ctx(ctx).Error().
				Interface("panic", rec).
				Bytes("stack", debug.Stack()).
				Msg("recovered from panic")

			writeJSON(ctx, w, http.StatusInternalServerError, responseError{
				Code:    http.StatusInternalServerError,
				Message: "internal server error",
			})
		}()
		next.ServeHTTP(w, r)
	})
}

func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		zerolog.Ctx(r.Context()).Debug().
			Str("method", r.Method).
			Str("url", r.URL.String()).
			Msg("request received")
		start := time.Now()
		next.ServeHTTP(w, r)
		zerolog.Ctx(r.Context()).Info().
			Str("method", r.Method).
			Str("url", r.URL.String()).
			Dur("duration", time.Since(start)).
			Msg("request completed")
	})
}

func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(requestIDHeader)
		if id == "" {
			id = uuid.New().String()
		}
		w.Header().Set(requestIDHeader, id)
		reqLogger := log.With().Str("request_id", id).Logger()
		ctx := reqLogger.WithContext(r.Context())
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
