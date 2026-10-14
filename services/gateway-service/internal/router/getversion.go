package router

import (
	_ "embed"
	"encoding/json"
	"gateway-service/internal/clients"
	"net/http"
	"strings"
)

//go:embed version.txt
var versionData string

func RegisterGetVersion(mux *http.ServeMux, c *clients.Clients) {
	mux.Handle("/getVersion", HandleVersion(c))

}

func HandleVersion(_ *clients.Clients) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"version": strings.TrimSpace(versionData),
		})
	}
}
