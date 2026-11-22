package httpapi

import "net/http"

type wifiRouter struct {
}

func (h *wifiRouter) healthCheck(w http.ResponseWriter, r *http.Request) {
	writeJSON(r.Context(), w, http.StatusOK, map[string]string{"status": "ok"})
}
