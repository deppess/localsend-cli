package handlers

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/deppes/localsend-cli/internal/config"
	"github.com/deppes/localsend-cli/internal/discovery"
	"github.com/deppes/localsend-cli/internal/protocol"
	"github.com/deppes/localsend-cli/internal/transfer"
)

var sessionCounter atomic.Int64

// PrepareUpload handles POST /api/localsend/v2/prepare-upload.
func (h *Handler) PrepareUpload(w http.ResponseWriter, r *http.Request) {
	ip := discovery.RemoteIP(r)
	if !h.filter.Allow(ip) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	var req protocol.PrepareUploadRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if req.Info.Alias == "" || len(req.Files) == 0 {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	sessionID := fmt.Sprintf("s%d", sessionCounter.Add(1))
	tokens := make(map[string]string, len(req.Files))
	fileNames := make(map[string]string, len(req.Files))

	for fileID, fi := range req.Files {
		tok := randomToken()
		tokens[fileID] = tok
		fileNames[fileID] = fi.FileName
	}

	s := &session{
		senderIP:   ip,
		tokens:     tokens,
		fileNames:  fileNames,
		tmpPaths:   make(map[string]string, len(req.Files)),
		totalCount: len(req.Files),
		expiry:     time.Now().Add(10 * time.Minute),
		done:       make(chan SessionResult, 1),
	}

	isFav := h.cfg.IsFavorite(ip)

	if h.notifier != nil {
		incoming := IncomingTransfer{
			SessionID: sessionID,
			FromIP:    ip,
			Alias:     req.Info.Alias,
			Files:     req.Files,
			IsFav:     isFav,
			Accept:    make(chan bool, 1),
			Done:      s.done,
		}

		var notifyCtx context.Context
		var notifyCancel context.CancelFunc
		if isFav || h.cfg.Receive.PromptTimeout <= 0 {
			// Favorites auto-accept; PromptTimeout=0 means wait forever.
			notifyCtx, notifyCancel = context.WithCancel(r.Context())
		} else {
			notifyCtx, notifyCancel = context.WithTimeout(r.Context(), time.Duration(h.cfg.Receive.PromptTimeout)*time.Second)
		}
		defer notifyCancel()

		if isFav {
			incoming.Accept <- true // pre-accept; Incoming() reads it immediately
		}

		accepted := h.notifier.Incoming(notifyCtx, incoming)
		if !accepted {
			http.Error(w, "rejected", http.StatusForbidden)
			return
		}
	}

	h.storeSession(sessionID, s)

	respFiles := make(map[string]string, len(tokens))
	for fileID, tok := range tokens {
		respFiles[fileID] = tok
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(protocol.PrepareUploadResponse{ //nolint:errcheck
		SessionID: sessionID,
		Files:     respFiles,
	})
}

// Upload handles POST /api/localsend/v2/upload.
func (h *Handler) Upload(w http.ResponseWriter, r *http.Request) {
	ip := discovery.RemoteIP(r)
	if !h.filter.Allow(ip) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	sessionID := r.URL.Query().Get("sessionId")
	fileID := r.URL.Query().Get("fileId")
	token := r.URL.Query().Get("token")

	if sessionID == "" || fileID == "" || token == "" {
		http.Error(w, "missing parameters", http.StatusBadRequest)
		return
	}

	s, ok := h.getSession(sessionID)
	if !ok {
		http.Error(w, "invalid session", http.StatusForbidden)
		return
	}

	// Verify sender IP matches the session.
	if s.senderIP != ip {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	// Verify token.
	expected, ok := s.tokens[fileID]
	if !ok || expected != token {
		http.Error(w, "invalid token", http.StatusForbidden)
		return
	}

	fileName, ok := s.fileNames[fileID]
	if !ok {
		http.Error(w, "unknown fileId", http.StatusBadRequest)
		return
	}

	dir := config.ExpandDir(h.cfg.Receive.Dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}

	// Sanitize fileName to prevent path traversal.
	fileName = filepath.Base(filepath.Clean(strings.ReplaceAll(fileName, "..", "")))
	if fileName == "." || fileName == "/" {
		http.Error(w, "invalid filename", http.StatusBadRequest)
		return
	}

	tmpPath := filepath.Join(dir, ".localsend-"+sessionID+"-"+fileName+".tmp")
	destPath := filepath.Join(dir, fileName)

	h.mu.Lock()
	s.tmpPaths[fileID] = tmpPath
	h.mu.Unlock()

	f, err := os.Create(tmpPath)
	if err != nil {
		h.mu.Lock()
		delete(s.tmpPaths, fileID)
		h.mu.Unlock()
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}

	_, err = transfer.CopyFile(r.Context(), f, r.Body, r.ContentLength, nil, fileName)
	f.Close()

	if err != nil {
		os.Remove(tmpPath)
		h.mu.Lock()
		delete(s.tmpPaths, fileID)
		h.mu.Unlock()
		http.Error(w, "transfer error", http.StatusInternalServerError)
		return
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		os.Remove(tmpPath)
		h.mu.Lock()
		delete(s.tmpPaths, fileID)
		h.mu.Unlock()
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}

	// Mark file complete and remove its token so it can't be replayed.
	h.mu.Lock()
	delete(s.tmpPaths, fileID)
	delete(s.tokens, fileID)
	s.completedNames = append(s.completedNames, fileName)
	remaining := len(s.tokens)
	completed := make([]string, len(s.completedNames))
	copy(completed, s.completedNames)
	total := s.totalCount
	h.mu.Unlock()

	if remaining == 0 {
		select {
		case s.done <- SessionResult{Files: completed, Total: total}:
		default:
		}
		h.deleteSession(sessionID)
	}

	w.WriteHeader(http.StatusOK)
}

func randomToken() string {
	b := make([]byte, 32)
	rand.Read(b) //nolint:errcheck
	return base64.RawURLEncoding.EncodeToString(b)
}
