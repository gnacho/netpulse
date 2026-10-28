package httpapi_test

import (
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

func TestFirmwareUploadRejectsOversizedMultipart(t *testing.T) {
	srv, _ := firmwareTestServer(t)
	id := addTestRouter(t, srv.db)
	cookie := adminCookieFor(t, srv)

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		defer pw.Close()
		part, err := mw.CreateFormFile("image", "big.bin")
		if err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		if _, err := io.CopyN(part, zeroReader{}, (100<<20)+(2<<20)); err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		if err := mw.Close(); err != nil {
			_ = pw.CloseWithError(err)
		}
	}()

	req, err := http.NewRequest("POST", srv.URL+"/api/firmware-upgrades/"+id+"/image", pr)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: "session", Value: cookie})

	client := &http.Client{Timeout: 60 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)

	if res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status: got %d want 413", res.StatusCode)
	}

	dir := filepath.Join(filepath.Dir(srv.db.Path), "firmware-uploads")
	if _, err := os.Stat(filepath.Join(dir, id+".bin")); !os.IsNotExist(err) {
		t.Fatalf("unexpected uploaded image: %v", err)
	}
	if tmps, err := filepath.Glob(filepath.Join(dir, "*.tmp")); err != nil {
		t.Fatalf("glob temporaries: %v", err)
	} else if len(tmps) != 0 {
		t.Fatalf("temporary files left behind: %v", tmps)
	}
}
