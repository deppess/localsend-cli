package handlers

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/deppes/localsend-cli/internal/config"
	"github.com/deppes/localsend-cli/internal/protocol"
	"github.com/deppes/localsend-cli/internal/whitelist"
)

// SessionResult carries the outcome of a completed or cancelled transfer session.
type SessionResult struct {
	Files []string // basenames of fully received files
	Total int      // total files expected in this session
	Err   error    // non-nil if cancelled or connection dropped mid-transfer
}

// IncomingTransfer represents a pending transfer request from a remote device.
type IncomingTransfer struct {
	SessionID string
	FromIP    string
	Alias     string
	Files     map[string]protocol.FileInfo
	IsFav     bool
	// Accept is written to by the notifier (true=accept, false=reject).
	Accept chan bool
	// Done receives the session result when all files are received or the transfer is cancelled.
	Done chan SessionResult
}

// Notifier is the interface between HTTP handlers and the UI layer.
type Notifier interface {
	// Incoming is called when a prepare-upload request arrives.
	// Returns true to accept, false to reject.
	Incoming(ctx context.Context, t IncomingTransfer) bool
}

// Handler holds shared state for all HTTP handlers.
type Handler struct {
	cfg      *config.Config
	filter   *whitelist.Filter
	notifier Notifier

	mu       sync.Mutex
	sessions map[string]*session
}

type session struct {
	senderIP       string
	tokens         map[string]string // fileID -> token
	fileNames      map[string]string // fileID -> fileName
	tmpPaths       map[string]string // fileID -> temp file path (cleaned up on cancel/error)
	completedNames []string          // basenames of fully received files
	totalCount     int               // total files expected
	expiry         time.Time
	done           chan SessionResult
}

// New creates a Handler.
func New(cfg *config.Config, filter *whitelist.Filter, notifier Notifier) *Handler {
	h := &Handler{
		cfg:      cfg,
		filter:   filter,
		notifier: notifier,
		sessions: make(map[string]*session),
	}
	go h.reapSessions()
	return h
}

func (h *Handler) reapSessions() {
	for range time.Tick(30 * time.Second) {
		now := time.Now()
		h.mu.Lock()
		for id, s := range h.sessions {
			if now.After(s.expiry) {
				for _, p := range s.tmpPaths {
					os.Remove(p) //nolint:errcheck
				}
				delete(h.sessions, id)
			}
		}
		h.mu.Unlock()
	}
}

func (h *Handler) storeSession(id string, s *session) {
	h.mu.Lock()
	h.sessions[id] = s
	h.mu.Unlock()
}

func (h *Handler) getSession(id string) (*session, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s, ok := h.sessions[id]
	return s, ok
}

func (h *Handler) deleteSession(id string) {
	h.mu.Lock()
	delete(h.sessions, id)
	h.mu.Unlock()
}
