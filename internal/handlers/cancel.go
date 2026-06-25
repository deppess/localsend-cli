package handlers

import (
	"fmt"
	"net/http"
	"os"

	"github.com/deppes/localsend-cli/internal/discovery"
)

// CancelHandler handles POST /api/localsend/v2/cancel.
func (h *Handler) CancelHandler(w http.ResponseWriter, r *http.Request) {
	ip := discovery.RemoteIP(r)
	if !h.filter.Allow(ip) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	sessionID := r.URL.Query().Get("sessionId")
	if sessionID == "" {
		http.Error(w, "missing sessionId", http.StatusBadRequest)
		return
	}

	s, ok := h.getSession(sessionID)
	if !ok {
		w.WriteHeader(http.StatusOK)
		return
	}
	if s.senderIP != ip {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	// Collect tmp paths and completed names before deleting the session.
	h.mu.Lock()
	var toDelete []string
	for _, p := range s.tmpPaths {
		toDelete = append(toDelete, p)
	}
	completed := make([]string, len(s.completedNames))
	copy(completed, s.completedNames)
	total := s.totalCount
	h.mu.Unlock()

	for _, p := range toDelete {
		os.Remove(p) //nolint:errcheck
	}

	select {
	case s.done <- SessionResult{Files: completed, Total: total, Err: fmt.Errorf("cancelled by sender")}:
	default:
	}
	h.deleteSession(sessionID)
	w.WriteHeader(http.StatusOK)
}
