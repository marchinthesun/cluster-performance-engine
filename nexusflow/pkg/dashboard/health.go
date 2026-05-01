package dashboard

import (
	"encoding/json"
	"net/http"
	"time"
)

type healthResponse struct {
	Status  string `json:"status"`
	Service string `json:"service"`
	Version string `json:"version"`
	TimeUTC string `json:"time_utc"`
}

func healthHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		resp := healthResponse{
			Status:  "ok",
			Service: "nexusflow-dashboard",
			Version: BinaryVersion(),
			TimeUTC: time.Now().UTC().Format(time.RFC3339Nano),
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
}
