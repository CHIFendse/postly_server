package router

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"strings"

	"gateway-service/internal/clients"
)

//go:embed version.txt
var versionData string

func RegisterGetVersion(mux *http.ServeMux, c *clients.Clients) {
	mux.Handle("/getVersion", HandleVersion(c))
}

// HandleVersion godoc
//	@Summary		Get current version
//	@Description	Returns current gateway service version for client-side compatibility checks
//	@Tags			version
//	@Produce		json
//	@Success		200	{object}	swagger.VersionResponse	"Current version information"
//	@Failure		405	{string}	string					"Method not allowed"
//	@Router			/getVersion [get]
func HandleVersion(_ *clients.Clients) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"version": strings.TrimSpace(versionData),
		})
	}
}