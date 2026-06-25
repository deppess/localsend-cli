package handlers

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/deppes/localsend-cli/internal/config"
	"github.com/deppes/localsend-cli/internal/protocol"
	"github.com/deppes/localsend-cli/internal/transfer"
	tlsutil "github.com/deppes/localsend-cli/internal/tls"
)

// SendOptions controls a send operation.
type SendOptions struct {
	TargetIP  string
	Paths     []string
	Self      protocol.DeviceInfo
	Cfg       *config.Config
	OnLog     func(string)
	OnProgress func(transfer.Progress)
}

// SendFiles prepares and uploads all files/dirs in opt.Paths to opt.TargetIP.
func SendFiles(ctx context.Context, opt SendOptions) error {
	files, err := collectFiles(opt.Paths)
	if err != nil {
		return fmt.Errorf("collect files: %w", err)
	}
	if len(files) == 0 {
		return fmt.Errorf("no files to send")
	}

	logf := func(msg string) {
		if opt.OnLog != nil {
			opt.OnLog(msg)
		}
	}

	client := newSendClient(opt.TargetIP, opt.Cfg)

	logf(fmt.Sprintf("preparing %d file(s)...", len(files)))
	resp, err := prepareUpload(ctx, client, opt.TargetIP, opt.Cfg.Device.Port, opt.Self, files)
	if err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	logf("upload accepted")

	sem := transfer.NewSemaphore(opt.Cfg.Discovery.UploadConcurrency)
	var wg sync.WaitGroup
	errCh := make(chan error, len(files))

	for _, f := range files {
		token, ok := resp.Files[f.ID]
		if !ok {
			continue // receiver skipped this file
		}
		wg.Add(1)
		sem.Acquire()
		go func(f fileEntry, token string) {
			defer wg.Done()
			defer sem.Release()
			logf(fmt.Sprintf("uploading %s", f.Name))
			if err := uploadFile(ctx, client, opt.TargetIP, opt.Cfg.Device.Port, resp.SessionID, f, token, opt.OnProgress); err != nil {
				errCh <- fmt.Errorf("%s: %w", f.Name, err)
			}
		}(f, token)
	}
	wg.Wait()
	close(errCh)

	var errs []error
	for e := range errCh {
		errs = append(errs, e)
	}
	if len(errs) > 0 {
		return errs[0]
	}
	logf("done")
	return nil
}

type fileEntry struct {
	ID   string
	Name string
	Path string
	Size int64
	SHA256 string
}

func collectFiles(paths []string) ([]fileEntry, error) {
	var files []fileEntry
	counter := 0
	for _, root := range paths {
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return err
			}
			hash, err := transfer.SHA256File(path)
			if err != nil {
				return err
			}
			files = append(files, fileEntry{
				ID:     fmt.Sprintf("file-%d", counter),
				Name:   info.Name(),
				Path:   path,
				Size:   info.Size(),
				SHA256: hash,
			})
			counter++
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return files, nil
}

func prepareUpload(ctx context.Context, client *http.Client, ip string, port int, self protocol.DeviceInfo, files []fileEntry) (*protocol.PrepareUploadResponse, error) {
	protoFiles := make(map[string]protocol.FileInfo, len(files))
	for _, f := range files {
		protoFiles[f.ID] = protocol.FileInfo{
			ID:       f.ID,
			FileName: f.Name,
			Size:     f.Size,
			FileType: filepath.Ext(f.Name),
			SHA256:   f.SHA256,
		}
	}

	body, _ := json.Marshal(protocol.PrepareUploadRequest{Info: self, Files: protoFiles})
	url := fmt.Sprintf("https://%s:%d/api/localsend/v2/prepare-upload", ip, port)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("rejected by receiver")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var out protocol.PrepareUploadResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &out, nil
}

func uploadFile(ctx context.Context, client *http.Client, ip string, port int, sessionID string, f fileEntry, token string, onProgress func(transfer.Progress)) error {
	file, err := os.Open(f.Path)
	if err != nil {
		return err
	}
	defer file.Close()

	pr, pw := io.Pipe()
	go func() {
		_, err := transfer.CopyFile(ctx, pw, file, f.Size, onProgress, f.Name)
		pw.CloseWithError(err)
	}()

	url := fmt.Sprintf("https://%s:%d/api/localsend/v2/upload?sessionId=%s&fileId=%s&token=%s",
		ip, port, sessionID, f.ID, token)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, pr)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = f.Size

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("upload failed: status %d", resp.StatusCode)
	}
	return nil
}

func newSendClient(peerIP string, cfg *config.Config) *http.Client {
	verifyFn := tlsutil.VerifyFunc(peerIP, cfg.Trusted, func(ip, fp string) {
		cfg.Trusted[ip] = fp
		config.Save(cfg) //nolint:errcheck
	})

	return &http.Client{
		Timeout: 30 * time.Minute,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify:    true,
				VerifyPeerCertificate: verifyFn,
			},
			MaxIdleConns:       10,
			IdleConnTimeout:    90 * time.Second,
			DisableCompression: true,
		},
	}
}
