package download

import "testing"

func TestCivitaiTokenURL(t *testing.T) {
	base := "https://civitai.com/api/download/models/123"
	if got := civitaiTokenURL(base, "abc"); got != base+"?token=abc" {
		t.Errorf("token append: got %q", got)
	}
	if got := civitaiTokenURL("https://x/y?a=1", "t k"); got != "https://x/y?a=1&token=t+k" {
		t.Errorf("existing query: got %q", got)
	}
	if got := civitaiTokenURL(base, ""); got != base {
		t.Errorf("no token should be unchanged: got %q", got)
	}
}

func TestRedactURL(t *testing.T) {
	if got := redactURL("https://civitai.com/api/download/models/1?token=secret"); got != "https://civitai.com/api/download/models/1?…" {
		t.Errorf("token not redacted: got %q", got)
	}
	if got := redactURL("https://hf.co/x/y"); got != "https://hf.co/x/y" {
		t.Errorf("no-query url should be unchanged: got %q", got)
	}
}

func TestPickCivitaiFile(t *testing.T) {
	files := []civitaiFile{
		{Name: "vae.safetensors", Type: "VAE", DownloadURL: "u1"},
		{Name: "model.safetensors", Type: "Model", Primary: true, DownloadURL: "u2"},
	}
	if f := pickCivitaiFile(files); f == nil || f.Name != "model.safetensors" {
		t.Errorf("expected primary model, got %+v", f)
	}
	// falls back to Model type when nothing is primary
	files[1].Primary = false
	if f := pickCivitaiFile(files); f == nil || f.Type != "Model" {
		t.Errorf("expected Model-type fallback, got %+v", f)
	}
	if f := pickCivitaiFile(nil); f != nil {
		t.Error("expected nil for no files")
	}
}

func TestResolve(t *testing.T) {
	cases := []struct {
		ref      string
		wantURL  string
		wantName string
		wantErr  bool
	}{
		{
			ref:      "hf:second-state/stable-diffusion-v1-5-GGUF/model-Q8_0.gguf",
			wantURL:  "https://huggingface.co/second-state/stable-diffusion-v1-5-GGUF/resolve/main/model-Q8_0.gguf",
			wantName: "model-Q8_0.gguf",
		},
		{
			ref:      "hf:owner/repo/sub/dir/file.safetensors",
			wantURL:  "https://huggingface.co/owner/repo/resolve/main/sub/dir/file.safetensors",
			wantName: "file.safetensors",
		},
		{
			ref:      "https://example.com/a/model.safetensors",
			wantURL:  "https://example.com/a/model.safetensors",
			wantName: "model.safetensors",
		},
		{ref: "hf:owner/repo", wantErr: true}, // missing file
		{ref: "civitai:12345", wantErr: true}, // unsupported
		{ref: "just-a-name", wantErr: true},
	}
	for _, c := range cases {
		url, name, err := Resolve(c.ref)
		if c.wantErr {
			if err == nil {
				t.Errorf("%q: expected error", c.ref)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: unexpected error: %v", c.ref, err)
			continue
		}
		if url != c.wantURL || name != c.wantName {
			t.Errorf("%q: got (%q,%q), want (%q,%q)", c.ref, url, name, c.wantURL, c.wantName)
		}
	}
}
