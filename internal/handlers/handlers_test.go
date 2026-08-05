package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/deppes/localsend-cli/internal/config"
	"github.com/deppes/localsend-cli/internal/protocol"
	"github.com/deppes/localsend-cli/internal/whitelist"
)

func newTestHandler(t *testing.T, filter *whitelist.Filter, notifier Notifier) (*Handler, *config.Config) {
	t.Helper()
	cfg := &config.Config{
		Receive:   config.ReceiveConfig{Dir: t.TempDir()},
		Discovery: config.DiscoveryConfig{UploadConcurrency: 4},
		Favorites: map[string]string{},
		Trusted:   map[string]string{},
	}
	if filter == nil {
		filter = whitelist.New(false, nil)
	}
	return New(cfg, filter, notifier), cfg
}

func prepareUploadBody(alias, fileID, fileName string, size int64) []byte {
	b, _ := json.Marshal(protocol.PrepareUploadRequest{
		Info: protocol.DeviceInfo{Alias: alias, Version: "2.1", DeviceType: "headless", Fingerprint: "fp", Port: 1, Protocol: "https"},
		Files: map[string]protocol.FileInfo{
			fileID: {ID: fileID, FileName: fileName, Size: size},
		},
	})
	return b
}

func doPrepareUpload(t *testing.T, h *Handler, remoteAddr string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/localsend/v2/prepare-upload", bytes.NewReader(body))
	req.RemoteAddr = remoteAddr
	rec := httptest.NewRecorder()
	h.PrepareUpload(rec, req)
	return rec
}

func TestPrepareUploadWhitelistForbidden(t *testing.T) {
	filter := whitelist.New(true, []string{"10.0.0.1"})
	h, _ := newTestHandler(t, filter, nil)

	rec := doPrepareUpload(t, h, "10.0.0.2:5555", prepareUploadBody("x", "f1", "a.txt", 1))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-whitelisted IP, got %d", rec.Code)
	}
}

func TestPrepareUploadWhitelistAllowed(t *testing.T) {
	filter := whitelist.New(true, []string{"10.0.0.1"})
	h, _ := newTestHandler(t, filter, nil)

	rec := doPrepareUpload(t, h, "10.0.0.1:5555", prepareUploadBody("x", "f1", "a.txt", 1))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for whitelisted IP, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPrepareUploadRateLimit(t *testing.T) {
	h, _ := newTestHandler(t, nil, nil)
	ip := "10.0.0.9:1234"

	for i := 0; i < prepareUploadRateLimit; i++ {
		body := prepareUploadBody("x", fmt.Sprintf("f%d", i), "a.txt", 1)
		rec := doPrepareUpload(t, h, ip, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d: %s", i, rec.Code, rec.Body.String())
		}
	}

	// One more from the same IP within the window must be throttled.
	rec := doPrepareUpload(t, h, ip, prepareUploadBody("x", "over-limit", "a.txt", 1))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after exceeding rate limit, got %d", rec.Code)
	}

	// A different source IP must be unaffected.
	rec = doPrepareUpload(t, h, "10.0.0.10:1234", prepareUploadBody("x", "other-ip", "a.txt", 1))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for a different source IP, got %d: %s", rec.Code, rec.Body.String())
	}
}

type capturingNotifier struct {
	gotAlias string
}

func (n *capturingNotifier) Incoming(_ context.Context, t IncomingTransfer) bool {
	n.gotAlias = t.Alias
	go func() { <-t.Done }() // drain so nothing blocks
	return true
}

func TestPrepareUploadSanitizesAlias(t *testing.T) {
	notifier := &capturingNotifier{}
	h, _ := newTestHandler(t, nil, notifier)

	rawAlias := "Evil\x1b[31mDevice"
	rec := doPrepareUpload(t, h, "10.0.0.5:1", prepareUploadBody(rawAlias, "f1", "a.txt", 1))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if notifier.gotAlias == rawAlias {
		t.Fatalf("alias was not sanitized: %q", notifier.gotAlias)
	}
	if notifier.gotAlias != "Evil[31mDevice" {
		t.Fatalf("unexpected sanitized alias: %q", notifier.gotAlias)
	}
}

func TestUploadTokenCompare(t *testing.T) {
	h, cfg := newTestHandler(t, nil, nil)
	const remoteAddr = "10.0.0.5:4321"
	const content = "hello world"

	rec := doPrepareUpload(t, h, remoteAddr, prepareUploadBody("x", "f1", "hello.txt", int64(len(content))))
	if rec.Code != http.StatusOK {
		t.Fatalf("prepare-upload: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp protocol.PrepareUploadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode prepare-upload response: %v", err)
	}
	token := resp.Files["f1"]
	if token == "" {
		t.Fatal("missing token in prepare-upload response")
	}

	t.Run("correct token succeeds and writes file", func(t *testing.T) {
		url := fmt.Sprintf("/api/localsend/v2/upload?sessionId=%s&fileId=f1&token=%s", resp.SessionID, token)
		req := httptest.NewRequest(http.MethodPost, url, bytes.NewReader([]byte(content)))
		req.RemoteAddr = remoteAddr
		rec := httptest.NewRecorder()
		h.Upload(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		data, err := os.ReadFile(filepath.Join(config.ExpandDir(cfg.Receive.Dir), "hello.txt"))
		if err != nil {
			t.Fatalf("reading received file: %v", err)
		}
		if string(data) != content {
			t.Fatalf("unexpected file content: %q", data)
		}
	})

	t.Run("wrong token is rejected and writes nothing", func(t *testing.T) {
		rec2 := doPrepareUpload(t, h, remoteAddr, prepareUploadBody("x", "f2", "other.txt", int64(len(content))))
		var resp2 protocol.PrepareUploadResponse
		if err := json.Unmarshal(rec2.Body.Bytes(), &resp2); err != nil {
			t.Fatalf("decode second prepare-upload response: %v", err)
		}

		url := fmt.Sprintf("/api/localsend/v2/upload?sessionId=%s&fileId=f2&token=wrong-token", resp2.SessionID)
		req := httptest.NewRequest(http.MethodPost, url, bytes.NewReader([]byte(content)))
		req.RemoteAddr = remoteAddr
		rec := httptest.NewRecorder()
		h.Upload(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for wrong token, got %d", rec.Code)
		}
		if _, err := os.Stat(filepath.Join(config.ExpandDir(cfg.Receive.Dir), "other.txt")); err == nil {
			t.Fatal("file should not have been written with a wrong token")
		}
	})
}
