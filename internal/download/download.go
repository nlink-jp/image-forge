// Package download fetches model files from Hugging Face, Civitai, or direct
// URLs. Tokens are supplied by the caller (never stored here).
package download

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"
)

// Resolve converts a model reference into a download URL and a suggested
// filename. Supported forms:
//
//	hf:owner/repo/path/to/file   -> Hugging Face resolve URL
//	https://... or http://...    -> used as-is
func Resolve(ref string) (url, filename string, err error) {
	switch {
	case strings.HasPrefix(ref, "hf:"):
		spec := strings.TrimPrefix(ref, "hf:")
		parts := strings.SplitN(spec, "/", 3)
		if len(parts) < 3 || parts[2] == "" {
			return "", "", fmt.Errorf("hf ref must be owner/repo/file, got %q", ref)
		}
		owner, repo, file := parts[0], parts[1], parts[2]
		url = fmt.Sprintf("https://huggingface.co/%s/%s/resolve/main/%s", owner, repo, file)
		return url, path.Base(file), nil
	case strings.HasPrefix(ref, "https://"), strings.HasPrefix(ref, "http://"):
		return ref, path.Base(ref), nil
	default:
		return "", "", fmt.Errorf("unsupported model reference %q (use hf:owner/repo/file or a URL)", ref)
	}
}

type civitaiFile struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Primary     bool   `json:"primary"`
	DownloadURL string `json:"downloadUrl"`
}

type civitaiVersion struct {
	Files []civitaiFile `json:"files"`
}

// CivitaiResolve looks up a Civitai model-version by id and returns the primary
// file's download URL (with the token embedded, since Civitai downloads require
// auth) and its filename. The token is also sent for the metadata request so
// gated versions resolve.
func CivitaiResolve(versionID, token string) (downloadURL, filename string, err error) {
	if strings.TrimSpace(versionID) == "" {
		return "", "", errors.New("civitai: a model-version id is required")
	}
	api := "https://civitai.com/api/v1/model-versions/" + url.PathEscape(versionID)
	req, err := http.NewRequest(http.MethodGet, api, nil)
	if err != nil {
		return "", "", err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("civitai: model-version %s: HTTP %d", versionID, resp.StatusCode)
	}
	var v civitaiVersion
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return "", "", fmt.Errorf("civitai: decode metadata: %w", err)
	}
	f := pickCivitaiFile(v.Files)
	if f == nil {
		return "", "", fmt.Errorf("civitai: no downloadable file for version %s", versionID)
	}
	return civitaiTokenURL(f.DownloadURL, token), f.Name, nil
}

// pickCivitaiFile chooses the primary file, else the first Model-type file, else
// the first file with a download URL.
func pickCivitaiFile(files []civitaiFile) *civitaiFile {
	for i := range files {
		if files[i].Primary && files[i].DownloadURL != "" {
			return &files[i]
		}
	}
	for i := range files {
		if files[i].Type == "Model" && files[i].DownloadURL != "" {
			return &files[i]
		}
	}
	for i := range files {
		if files[i].DownloadURL != "" {
			return &files[i]
		}
	}
	return nil
}

// redactURL drops the query string so tokens (e.g. ?token=) never leak into logs
// or error messages.
func redactURL(u string) string {
	if i := strings.IndexByte(u, '?'); i >= 0 {
		return u[:i] + "?…"
	}
	return u
}

// civitaiTokenURL appends the API token as a query parameter — Civitai's download
// auth. (It processes the token before redirecting to a signed CDN URL.)
func civitaiTokenURL(base, token string) string {
	if token == "" {
		return base
	}
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	return base + sep + "token=" + url.QueryEscape(token)
}

// Fetch downloads url to dest atomically (via a .part file), resuming a partial
// .part with an HTTP Range request and retrying transient failures (large model
// downloads routinely hit dropped connections). progress is called with a 0..1
// fraction when the total size is known. token, if non-empty, is a bearer token.
func Fetch(url, dest, token string, progress func(float64)) error {
	const maxAttempts = 5
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := fetchOnce(url, dest, token, progress); err != nil {
			lastErr = err
			if attempt < maxAttempts {
				time.Sleep(time.Duration(attempt) * 2 * time.Second)
			}
			continue
		}
		return nil
	}
	return lastErr
}

func fetchOnce(url, dest, token string, progress func(float64)) error {
	tmp := dest + ".part"
	var start int64
	if fi, serr := os.Stat(tmp); serr == nil {
		start = fi.Size()
	}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if start > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", start))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var (
		f            *os.File
		written, tot int64
	)
	switch resp.StatusCode {
	case http.StatusPartialContent: // resume from `start`
		f, err = os.OpenFile(tmp, os.O_APPEND|os.O_WRONLY, 0o644)
		written, tot = start, start+resp.ContentLength
	case http.StatusOK: // server ignored Range — download from scratch
		f, err = os.Create(tmp)
		written, tot = 0, resp.ContentLength
	default:
		return fmt.Errorf("download %s: HTTP %d", redactURL(url), resp.StatusCode)
	}
	if err != nil {
		return err
	}

	buf := make([]byte, 1<<20)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				return werr
			}
			written += int64(n)
			if progress != nil && tot > 0 {
				progress(float64(written) / float64(tot))
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			return rerr
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}
