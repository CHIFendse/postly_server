package handlers

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"strings"
)
//go:embed version.txt
var versionData string

func HandleVersion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"version": strings.TrimSpace(versionData),
	})
}
