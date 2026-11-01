package httpapi

import "net/http"

type SimpleWS struct {
}

func NewSimpleWS() *SimpleWS {
	return &SimpleWS{}
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
