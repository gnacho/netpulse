package rintegration

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	githubAPIBase = "https://api.github.com"
	maxDownload   = 64 << 20 // bytes
	fetchTimeout  = 60 * time.Second
	userAgent     = "netpulse"
)

// Fetcher resolves and downloads verified integration releases.
type Fetcher struct {
	Client  *http.Client
	APIBase string // override for tests; default githubAPIBase
}

// NewFetcher builds a Fetcher with the default 60s timeout.
func NewFetcher() *Fetcher {
	return &Fetcher{Client: &http.Client{Timeout: fetchTimeout}, APIBase: githubAPIBase}
}

func (f *Fetcher) client() *http.Client {
	if f.Client != nil {
		return f.Client
	}
	return &http.Client{Timeout: fetchTimeout}
}

func (f *Fetcher) base() string {
	if f.APIBase != "" {
		return f.APIBase
	}
	return githubAPIBase
}

// Resolved is a verified release asset: the tarball URL and the digest its
// publisher shipped next to it (the .sha256 companion asset).
type Resolved struct {
	Version   string
	AssetName string
	URL       string
	SHA256    string
}

// ResolveLatest finds the first non-prerelease release whose tag starts with
// the vendor's prefix and returns the asset matching the requested arch plus
// its published sha256.
func (f *Fetcher) ResolveLatest(v Vendor, arch string) (Resolved, error) {
	var zero Resolved
	url := f.base() + "/repos/" + v.Repo + "/releases"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return zero, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent)
	resp, err := f.client().Do(req)
	if err != nil {
		return zero, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return zero, fmt.Errorf("github releases: %s", resp.Status)
	}
	var releases []struct {
		TagName    string `json:"tag_name"`
		Prerelease bool   `json:"prerelease"`
		Assets     []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&releases); err != nil {
		return zero, err
	}
	for _, rel := range releases {
		if rel.Prerelease || !strings.HasPrefix(rel.TagName, v.TagPrefix) {
			continue
		}
		version := strings.TrimPrefix(rel.TagName, v.TagPrefix)
		assetName := v.AssetName(version, arch)
		assetURL, shaURL := "", ""
		for _, a := range rel.Assets {
			switch a.Name {
			case assetName:
				assetURL = a.URL
			case assetName + ".sha256":
				shaURL = a.URL
			}
		}
		if assetURL == "" || shaURL == "" {
			continue
		}
		digest, err := f.fetchSHA256(shaURL)
		if err != nil {
			return zero, err
		}
		return Resolved{Version: version, AssetName: assetName, URL: assetURL, SHA256: digest}, nil
	}
	return zero, fmt.Errorf("no release found for prefix %q", v.TagPrefix)
}

func (f *Fetcher) fetchSHA256(url string) (string, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := f.client().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("sha256 download: %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return "", err
	}
	return parseSHA256(string(body))
}

// parseSHA256 extracts the leading hex digest from a .sha256 file, which may
// be either the bare hash or "hash  filename" (sha256sum output).
func parseSHA256(s string) (string, error) {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return "", errors.New("empty sha256")
	}
	h := strings.ToLower(fields[0])
	if len(h) != 64 || !isHex(h) {
		return "", errors.New("invalid sha256")
	}
	return h, nil
}

func isHex(s string) bool {
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

// Verify downloads the asset and checks its sha256 against the published
// digest, then discards the bytes. Used for ssh-mode installs, where the
// router downloads the asset itself from the URL the server has verified.
func (f *Fetcher) Verify(res Resolved) error {
	tmp, err := f.download(res)
	if err != nil {
		return err
	}
	return os.Remove(tmp)
}

// Download fetches the asset, verifies its sha256 and extracts the tar.gz
// into destDir. It returns the path to the extracted binary.
func (f *Fetcher) Download(res Resolved, binary, destDir string) (string, error) {
	tmp, err := f.download(res)
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp)
	fh, err := os.Open(tmp)
	if err != nil {
		return "", err
	}
	defer fh.Close()
	extracted, err := extractTarGz(fh, destDir)
	if err != nil {
		return "", err
	}
	for _, p := range extracted {
		if filepath.Base(p) == binary {
			return p, nil
		}
	}
	return "", fmt.Errorf("binary %q not found in archive", binary)
}

// download fetches the asset into a temp file, enforcing maxDownload bytes and
// verifying the sha256 against the published digest before returning the path.
func (f *Fetcher) download(res Resolved) (string, error) {
	req, err := http.NewRequest("GET", res.URL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := f.client().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download: %s", resp.Status)
	}
	tmp, err := os.CreateTemp("", "rintegration-*.tar.gz")
	if err != nil {
		return "", err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(resp.Body, maxDownload+1))
	if err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", err
	}
	tmp.Close()
	if n > maxDownload {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("asset exceeds %d bytes", maxDownload)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, res.SHA256) {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("sha256 mismatch: got %s, want %s", got, res.SHA256)
	}
	return tmp.Name(), nil
}

// extractTarGz extracts a gzipped tarball into destDir, refusing absolute
// paths and any entry that could escape destDir (".."). It returns the paths
// of the extracted regular files.
func extractTarGz(r io.Reader, destDir string) ([]string, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var out []string
	cleanDest := filepath.Clean(destDir)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		name := hdr.Name
		if name == "" || filepath.IsAbs(name) || strings.Contains(name, "..") {
			return nil, fmt.Errorf("unsafe path in archive: %q", hdr.Name)
		}
		target := filepath.Join(destDir, name)
		if !strings.HasPrefix(filepath.Clean(target), cleanDest+string(os.PathSeparator)) {
			return nil, fmt.Errorf("path escapes dest dir: %q", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return nil, err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return nil, err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
			if err != nil {
				return nil, err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return nil, err
			}
			if err := f.Close(); err != nil {
				return nil, err
			}
			out = append(out, target)
		}
	}
	return out, nil
}
