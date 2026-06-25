package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/deppes/localsend-cli/internal/protocol"
)

// InfoHandler handles GET /api/localsend/v2/info.
func InfoHandler(self protocol.DeviceInfo) http.HandlerFunc {
	data, _ := json.Marshal(self)
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(data) //nolint:errcheck
	}
}
