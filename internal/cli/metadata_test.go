package cli

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nlink-jp/image-forge/internal/engine"
)

// withVersion pins binVersion for deterministic assertions and restores it.
func withVersion(t *testing.T, v string) {
	t.Helper()
	prev := binVersion
	binVersion = v
	t.Cleanup(func() { binVersion = prev })
}

func TestBuildImageMetadata_EmbedFalseNil(t *testing.T) {
	if got := buildImageMetadata(engine.Request{Prompt: "x"}, "m", "", false); got != nil {
		t.Fatalf("embed=false should return nil, got %v", got)
	}
}

// metadataBuilder is what the engine calls per output image so a --batch records
// each image's own seed (sd.cpp uses base+b). Each call must re-embed the given
// seed, not the base — this guards the reproducibility bug (#1).
func TestMetadataBuilderRecordsPerImageSeed(t *testing.T) {
	base := engine.Request{Prompt: "x", Seed: 100, Steps: 20, CFG: 7, Width: 512, Height: 512, Sampler: "euler"}
	build := metadataBuilder(base, "m", "", true)
	for _, seed := range []int64{100, 101, 142} {
		chunks := build(seed)
		var got int64 = -1
		for _, c := range chunks {
			if c.Keyword == "image-forge" {
				var meta map[string]any
				if err := json.Unmarshal([]byte(c.Text), &meta); err != nil {
					t.Fatalf("bad json: %v", err)
				}
				got = int64(meta["seed"].(float64))
			}
		}
		if got != seed {
			t.Errorf("build(%d) recorded seed %d, want %d", seed, got, seed)
		}
	}
	// The base request must be unchanged (the builder copies it per call).
	if base.Seed != 100 {
		t.Errorf("builder mutated the base request seed: %d", base.Seed)
	}
	// embed=false yields no chunks regardless of seed.
	if got := metadataBuilder(base, "m", "", false)(999); got != nil {
		t.Errorf("embed=false builder should return nil, got %v", got)
	}
}

func TestBuildImageMetadata_Txt2Img(t *testing.T) {
	withVersion(t, "v9.9.9")
	req := engine.Request{
		Prompt:   "a cat",
		Seed:     20240707,
		Steps:    26,
		CFG:      7,
		Width:    1024,
		Height:   1024,
		Sampler:  "euler_a",
		ClipSkip: 2,
	}
	texts := buildImageMetadata(req, "prefect-pony-xl", "", true)
	if len(texts) != 2 {
		t.Fatalf("want 2 chunks, got %d", len(texts))
	}
	if texts[0].Keyword != "parameters" || texts[1].Keyword != "image-forge" {
		t.Fatalf("unexpected keywords: %q, %q", texts[0].Keyword, texts[1].Keyword)
	}

	params := texts[0].Text
	// No negative-prompt line when Negative is empty.
	if strings.Contains(params, "Negative prompt:") {
		t.Errorf("unexpected negative-prompt line in:\n%s", params)
	}
	// A1111 settings line present with the expected fields.
	for _, want := range []string{
		"a cat\n",
		"Steps: 26", "Sampler: euler_a", "CFG scale: 7",
		"Seed: 20240707", "Size: 1024x1024", "Model: prefect-pony-xl",
		"Clip skip: 2", "Version: image-forge v9.9.9",
	} {
		if !strings.Contains(params, want) {
			t.Errorf("parameters missing %q in:\n%s", want, params)
		}
	}
	if strings.Contains(params, "Denoising strength") || strings.Contains(params, "Hires") {
		t.Errorf("txt2img should not carry Denoising/Hires:\n%s", params)
	}

	// The image-forge entry is valid JSON containing the model + seed.
	var m map[string]any
	if err := json.Unmarshal([]byte(texts[1].Text), &m); err != nil {
		t.Fatalf("image-forge JSON invalid: %v\n%s", err, texts[1].Text)
	}
	if m["model"] != "prefect-pony-xl" {
		t.Errorf("json model = %v, want prefect-pony-xl", m["model"])
	}
	if m["seed"].(float64) != 20240707 {
		t.Errorf("json seed = %v, want 20240707", m["seed"])
	}
	if _, ok := m["img2img"]; ok {
		t.Errorf("txt2img JSON should omit img2img: %s", texts[1].Text)
	}
	if _, ok := m["hires"]; ok {
		t.Errorf("txt2img JSON should omit hires: %s", texts[1].Text)
	}
}

func TestBuildImageMetadata_ClipSkip1Omitted(t *testing.T) {
	req := engine.Request{Prompt: "p", Steps: 20, Width: 512, Height: 512, ClipSkip: 1}
	params := buildImageMetadata(req, "m", "", true)[0].Text
	if strings.Contains(params, "Clip skip") {
		t.Errorf("clip skip 1 should be omitted:\n%s", params)
	}
}

func TestBuildImageMetadata_Img2ImgAddsDenoising(t *testing.T) {
	req := engine.Request{
		Prompt: "a dog", Negative: "blurry",
		Seed: 42, Steps: 30, CFG: 6.5, Width: 768, Height: 768, Sampler: "dpmpp2m",
		InitImage: "in.png", Strength: 0.4,
	}
	texts := buildImageMetadata(req, "juggernaut-xl", "", true)
	params := texts[0].Text
	if !strings.Contains(params, "\nNegative prompt: blurry\n") {
		t.Errorf("expected negative-prompt line:\n%s", params)
	}
	if !strings.Contains(params, "Denoising strength: 0.4") {
		t.Errorf("expected Denoising strength: 0.4 in:\n%s", params)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(texts[1].Text), &m); err != nil {
		t.Fatal(err)
	}
	sub, ok := m["img2img"].(map[string]any)
	if !ok {
		t.Fatalf("expected img2img object: %s", texts[1].Text)
	}
	if sub["strength"].(float64) != 0.4 {
		t.Errorf("img2img record wrong: %v", sub)
	}
	// The init image itself must never be recorded (ADR-0005: no paths, no inputs).
	if _, present := sub["init"]; present {
		t.Errorf("img2img must not record the init image: %v", sub)
	}
}

func TestBuildImageMetadata_HiresFields(t *testing.T) {
	req := engine.Request{
		Prompt: "p", Steps: 24, Width: 832, Height: 1216, Sampler: "euler",
		Hires: true, HiresScale: 1.5, HiresDenoise: 0.5, HiresUpscaler: "latent", HiresSteps: 12,
	}
	texts := buildImageMetadata(req, "m", "v", true)
	params := texts[0].Text
	if !strings.Contains(params, "Hires upscale: 1.5") || !strings.Contains(params, "Hires upscaler: latent") {
		t.Errorf("expected hires fields in:\n%s", params)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(texts[1].Text), &m); err != nil {
		t.Fatal(err)
	}
	if m["prediction"] != "v" {
		t.Errorf("json prediction = %v, want v", m["prediction"])
	}
	h, ok := m["hires"].(map[string]any)
	if !ok {
		t.Fatalf("expected hires object: %s", texts[1].Text)
	}
	if h["scale"].(float64) != 1.5 || h["upscaler"] != "latent" {
		t.Errorf("hires record wrong: %v", h)
	}
}

func TestBuildUpscaleMetadata(t *testing.T) {
	withVersion(t, "v1.2.3")
	// Source path doesn't exist → light record with just the upscale sub-record.
	texts := buildUpscaleMetadata("realesrgan-x4plus", "/models/x4.pth", 4, "/tmp/in.png")
	if len(texts) != 1 || texts[0].Keyword != "image-forge" {
		t.Fatalf("want one image-forge chunk, got %v", texts)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(texts[0].Text), &m); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, texts[0].Text)
	}
	up, _ := m["upscale"].(map[string]any)
	if up == nil || up["upscaler"] != "realesrgan-x4plus" || up["factor"].(float64) != 4 ||
		up["source"] != "in.png" || m["version"] != "v1.2.3" {
		t.Errorf("upscale record wrong: %v", m)
	}
	// Unnamed upscaler falls back to the model-file base name.
	texts2 := buildUpscaleMetadata("", "/models/x4-anime.pth", 0, "in.jpg")
	var m2 map[string]any
	_ = json.Unmarshal([]byte(texts2[0].Text), &m2)
	up2, _ := m2["upscale"].(map[string]any)
	if up2 == nil || up2["upscaler"] != "x4-anime" {
		t.Errorf("unnamed upscaler = %v, want x4-anime", m2["upscale"])
	}
}

// A source PNG's generation metadata (prompt / seed / params) is carried through
// into the upscaled PNG, plus the upscale sub-record and the parameters chunk.
func TestBuildUpscaleMetadataCarriesSource(t *testing.T) {
	withVersion(t, "v9")
	forge := `{"version":"v1","model":"realvisxl-v5","prompt":"a cat",` +
		`"negative":"blurry","seed":42,"steps":30,"width":768,"height":768,"sampler":"euler_a"}`
	src := writePNGWithText(t, map[string]string{
		"image-forge": forge,
		"parameters":  "a cat\nNegative prompt: blurry\nSteps: 30, Seed: 42",
	})

	texts := buildUpscaleMetadata("realesrgan-x4plus", "/models/x4.pth", 4, src)
	byKw := map[string]string{}
	for _, tx := range texts {
		byKw[tx.Keyword] = tx.Text
	}

	var m imgforgeMeta
	if err := json.Unmarshal([]byte(byKw["image-forge"]), &m); err != nil {
		t.Fatalf("bad image-forge json: %v", err)
	}
	if m.Prompt != "a cat" || m.Seed != 42 || m.Model != "realvisxl-v5" || m.Width != 768 {
		t.Errorf("source metadata not carried: %+v", m)
	}
	if m.Upscale == nil || m.Upscale.Upscaler != "realesrgan-x4plus" || m.Upscale.Factor != 4 {
		t.Errorf("upscale sub-record wrong: %+v", m.Upscale)
	}
	if m.Version != "v9" {
		t.Errorf("version not stamped with upscaling binary: %q", m.Version)
	}
	if !strings.Contains(byKw["parameters"], "a cat") {
		t.Errorf("parameters chunk not carried: %q", byKw["parameters"])
	}
}

// writePNGWithText writes a minimal structurally-valid PNG carrying the given
// tEXt chunks (enough for engine.ReadPNGText to walk), returning its temp path.
func writePNGWithText(t *testing.T, chunks map[string]string) string {
	t.Helper()
	var buf bytes.Buffer
	buf.Write([]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a})
	buf.Write(rawChunk("IHDR", []byte{0, 0, 0, 1, 0, 0, 0, 1, 8, 2, 0, 0, 0}))
	for kw, text := range chunks {
		data := append([]byte(kw), 0)
		data = append(data, []byte(text)...)
		buf.Write(rawChunk("tEXt", data))
	}
	buf.Write(rawChunk("IEND", nil))
	path := filepath.Join(t.TempDir(), "src.png")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// rawChunk assembles one PNG chunk: length(4 BE) + type + data + CRC-32(4 BE).
func rawChunk(typ string, data []byte) []byte {
	td := append([]byte(typ), data...)
	var u32 [4]byte
	var out []byte
	binary.BigEndian.PutUint32(u32[:], uint32(len(data)))
	out = append(out, u32[:]...)
	out = append(out, td...)
	binary.BigEndian.PutUint32(u32[:], crc32.ChecksumIEEE(td))
	out = append(out, u32[:]...)
	return out
}

func TestModelDisplayName(t *testing.T) {
	if got := modelDisplayName("animagine-xl-4", ""); got != "animagine-xl-4" {
		t.Errorf("name case = %q", got)
	}
	if got := modelDisplayName("", "/a/b/model.safetensors"); got != "model" {
		t.Errorf("path case = %q", got)
	}
	if got := modelDisplayName("", ""); got != "" {
		t.Errorf("empty case = %q", got)
	}
}
