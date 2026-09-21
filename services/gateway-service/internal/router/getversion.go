package router

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"strings"
	"gateway-service/internal/clients"
	"time"
	"fmt"
	
)
//go:embed version.txt
var versionData string

func RegisterGetVersion(mux *http.ServeMux, c *clients.Clients) {
	fmt.Printf("LOG [%s], success initialize getVersion", time.Now())
	mux.HandleFunc("/getVersion",  HandleVersion(c))

}

func HandleVersion(_ *clients.Clients) http.HandlerFunc {
	fmt.Printf("LOG [%s], success getVersion", time.Now())
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"version": strings.TrimSpace(versionData),
		})
	}
}

