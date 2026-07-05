package download

import "testing"

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
