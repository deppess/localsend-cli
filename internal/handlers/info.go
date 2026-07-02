package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/deppes/localsend-cli/internal/discovery"
	"github.com/deppes/localsend-cli/internal/protocol"
	"github.com/deppes/localsend-cli/internal/whitelist"
)

// InfoHandler handles GET /api/localsend/v2/info.
func InfoHandler(self protocol.DeviceInfo, filter *whitelist.Filter) http.HandlerFunc {
	data, _ := json.Marshal(self)
	return func(w http.ResponseWriter, r *http.Request) {
		if !filter.Allow(discovery.RemoteIP(r)) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(data) //nolint:errcheck
	}
}
