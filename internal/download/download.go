// Package download fetches model files from Hugging Face, Civitai, or direct
// URLs. Tokens are supplied by the caller (never stored here).
package download

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
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

// Fetch downloads url to dest atomically (via a .part file). If the response
// carries a content length, progress is called with a 0..1 fraction. token, if
// non-empty, is sent as a bearer token (Hugging Face / Civitai).
func Fetch(url, dest, token string, progress func(float64)) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}

	tmp := dest + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	total := resp.ContentLength
	var read int64
	buf := make([]byte, 1<<20)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				return werr
			}
			read += int64(n)
			if progress != nil && total > 0 {
				progress(float64(read) / float64(total))
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
