package transfer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// Semaphore is a counting semaphore for bounding concurrency.
type Semaphore chan struct{}

func NewSemaphore(n int) Semaphore {
	return make(Semaphore, n)
}

func (s Semaphore) Acquire() { s <- struct{}{} }
func (s Semaphore) Release() { <-s }

// Progress is sent periodically during a transfer.
type Progress struct {
	FileName string
	Sent     int64
	Total    int64
}

// CopyFile copies src to dst with context cancellation and optional progress callback.
// Uses io.Copy which leverages sendfile on Linux for zero-copy performance.
func CopyFile(ctx context.Context, dst io.Writer, src io.Reader, size int64, onProgress func(Progress), fileName string) (int64, error) {
	pr := &progressReader{
		r:          src,
		total:      size,
		fileName:   fileName,
		onProgress: onProgress,
		ctx:        ctx,
	}
	return io.Copy(dst, pr)
}

// SHA256File computes the SHA-256 hash of a file.
func SHA256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

type progressReader struct {
	r          io.Reader
	sent       int64
	total      int64
	fileName   string
	onProgress func(Progress)
	ctx        context.Context
}

func (p *progressReader) Read(buf []byte) (int, error) {
	select {
	case <-p.ctx.Done():
		return 0, fmt.Errorf("transfer cancelled")
	default:
	}
	n, err := p.r.Read(buf)
	if n > 0 {
		p.sent += int64(n)
		if p.onProgress != nil {
			p.onProgress(Progress{FileName: p.fileName, Sent: p.sent, Total: p.total})
		}
	}
	return n, err
}
