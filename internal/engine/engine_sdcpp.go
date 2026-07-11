//go:build cgo_sdcpp

package engine

/*
#cgo CFLAGS: -I${SRCDIR}/../../third_party/stable-diffusion.cpp/include
#cgo LDFLAGS: ${SRCDIR}/../../third_party/stable-diffusion.cpp/build/libstable-diffusion.a
#cgo LDFLAGS: ${SRCDIR}/../../third_party/stable-diffusion.cpp/build/ggml/src/libggml.a
#cgo LDFLAGS: ${SRCDIR}/../../third_party/stable-diffusion.cpp/build/ggml/src/libggml-cpu.a
#cgo LDFLAGS: ${SRCDIR}/../../third_party/stable-diffusion.cpp/build/ggml/src/ggml-metal/libggml-metal.a
#cgo LDFLAGS: ${SRCDIR}/../../third_party/stable-diffusion.cpp/build/ggml/src/ggml-blas/libggml-blas.a
#cgo LDFLAGS: ${SRCDIR}/../../third_party/stable-diffusion.cpp/build/ggml/src/libggml-base.a
#cgo LDFLAGS: -lc++ -framework Foundation -framework Metal -framework MetalKit -framework Accelerate
#include <stdlib.h>
#include <stable-diffusion.h>

// Bridge so the Go-exported progress callback can be registered as a C function
// pointer of the exact sd_progress_cb_t signature.
extern void goProgress(int step, int steps, float t, void* data);
static void ifg_set_progress(void* data) { sd_set_progress_callback(goProgress, data); }

// A no-op progress callback keeps sd.cpp's built-in progress printer SILENT.
// With NO callback registered, sd.cpp printf's a "|####| N/M - X MB/s" bar to
// stdout — notably during model load in new_sd_ctx (before Render sets the real
// callback). That is invisible in a TTY (it's a \r-updated line that flashes by)
// but is preserved verbatim on a pipe, corrupting machine stdout consumers such
// as the MCP JSON-RPC stream. Registering a no-op makes sd.cpp route progress to
// the callback (which discards) instead of printing. We keep this installed
// whenever we are not actively rendering, so sd_progress_cb is never NULL.
static void ifg_noop_progress(int step, int steps, float t, void* data) { (void)step; (void)steps; (void)t; (void)data; }
static void ifg_silence_progress(void)   { sd_set_progress_callback(ifg_noop_progress, NULL); }
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // register JPEG decoder for init images
	_ "image/png"  // register PNG decoder for init images (encoding is done by encodePNGWithText)
	"os"
	"runtime/cgo"
	"strings"
	"unsafe"
)

// quantTypes maps CLI quant names to sd.cpp weight types.
var quantTypes = map[string]C.enum_sd_type_t{
	"q8_0": C.SD_TYPE_Q8_0, "q5_0": C.SD_TYPE_Q5_0, "q5_1": C.SD_TYPE_Q5_1,
	"q4_0": C.SD_TYPE_Q4_0, "q4_1": C.SD_TYPE_Q4_1,
	"q2_k": C.SD_TYPE_Q2_K, "q3_k": C.SD_TYPE_Q3_K, "q4_k": C.SD_TYPE_Q4_K,
	"q5_k": C.SD_TYPE_Q5_K, "q6_k": C.SD_TYPE_Q6_K,
	"f16": C.SD_TYPE_F16, "f32": C.SD_TYPE_F32,
}

// Quantize converts a model to a GGUF of the given quant type (e.g. "q8_0",
// "q4_k"), optionally baking in a VAE.
func Quantize(inputPath, vaePath, outputPath, quantType string) error {
	t, ok := quantTypes[strings.ToLower(quantType)]
	if !ok {
		return fmt.Errorf("sdcpp: unknown quant type %q", quantType)
	}
	// sd.cpp's convert wraps these in std::string, so they must not be NULL —
	// pass empty C strings when absent.
	cIn := C.CString(inputPath)
	defer C.free(unsafe.Pointer(cIn))
	cOut := C.CString(outputPath)
	defer C.free(unsafe.Pointer(cOut))
	cVae := C.CString(vaePath)
	defer C.free(unsafe.Pointer(cVae))
	cRules := C.CString("")
	defer C.free(unsafe.Pointer(cRules))
	if !bool(C.convert(cIn, cVae, cOut, t, cRules, C.bool(true))) {
		return fmt.Errorf("sdcpp: quantization to %s failed", quantType)
	}
	return nil
}

// Info returns build/system info from the linked stable-diffusion.cpp runtime.
func Info() string {
	return "engine: stable-diffusion.cpp — " + C.GoString(C.sd_get_system_info())
}

// Open loads a model into a resident context. The heavy cost (model read + Metal
// init) is paid here once; Render reuses it.
func Open(p OpenParams) (Session, error) {
	if p.ModelPath == "" && p.DiffusionModel == "" {
		return nil, errors.New("sdcpp: a model path or diffusion model is required")
	}
	var cp C.sd_ctx_params_t
	C.sd_ctx_params_init(&cp)

	// LoRA application mode. sd.cpp's LORA_APPLY_AUTO picks "immediately" for
	// non-quantized weights, which merges the LoRA into the model params up front
	// (ModelManager::apply_loras_to_params). That path segfaults on some LoRAs
	// (e.g. UNet-only SDXL LCM-LoRAs). at_runtime applies the LoRA during the
	// forward pass instead, which is robust; the cost is per-step compute rather
	// than a one-off merge. Correctness over speed.
	cp.lora_apply_mode = C.LORA_APPLY_AT_RUNTIME

	// Set each non-empty path; the CStrings must outlive new_sd_ctx, so free them
	// only after it returns.
	var frees []unsafe.Pointer
	set := func(s string) *C.char {
		if s == "" {
			return nil
		}
		c := C.CString(s)
		frees = append(frees, unsafe.Pointer(c))
		return c
	}
	defer func() {
		for _, f := range frees {
			C.free(f)
		}
	}()

	cp.model_path = set(p.ModelPath)
	cp.diffusion_model_path = set(p.DiffusionModel)
	cp.clip_l_path = set(p.ClipL)
	cp.clip_g_path = set(p.ClipG)
	cp.t5xxl_path = set(p.T5XXL)
	cp.llm_path = set(p.LLM)
	cp.vae_path = set(p.VAEPath)
	cp.control_net_path = set(p.ControlNet)

	// Empty prediction leaves the init default (PREDICTION_COUNT = auto-detect);
	// "v" forces v-prediction for models sd.cpp can't auto-detect.
	if p.Prediction != "" {
		cPred := C.CString(p.Prediction)
		cp.prediction = C.str_to_prediction(cPred)
		C.free(unsafe.Pointer(cPred))
	}

	// Silence sd.cpp's built-in stdout progress bar during the model read: with a
	// callback registered (here a no-op), sd.cpp routes load progress to it rather
	// than printf-ing "|####| MB/s" to stdout. Render swaps in the real event
	// callback for generation, then restores the no-op.
	C.ifg_silence_progress()

	ctx := C.new_sd_ctx(&cp)
	if ctx == nil {
		name := p.ModelPath
		if name == "" {
			name = p.DiffusionModel
		}
		return nil, fmt.Errorf("sdcpp: failed to load model %q", name)
	}
	return &sdSession{ctx: ctx}, nil
}

type sdSession struct{ ctx *C.sd_ctx_t }

func (s *sdSession) Close() error {
	if s.ctx != nil {
		C.free_sd_ctx(s.ctx)
		s.ctx = nil
	}
	return nil
}

//export goProgress
func goProgress(step, steps C.int, t C.float, data unsafe.Pointer) {
	if data == nil {
		return
	}
	ch, ok := cgo.Handle(data).Value().(chan<- Event)
	if !ok {
		return
	}
	var p float64
	if steps > 0 {
		p = float64(step) / float64(steps)
	}
	ch <- Event{Kind: "progress", Progress: p, Message: fmt.Sprintf("step %d/%d", int(step), int(steps))}
}

// Render generates from the loaded model. The caller must read from events
// concurrently. ModelPath/VAEPath on req are ignored (the model is already open).
func (s *sdSession) Render(ctx context.Context, req Request, events chan<- Event) error {
	if s.ctx == nil {
		return errors.New("sdcpp: session is closed")
	}

	var g C.sd_img_gen_params_t
	C.sd_img_gen_params_init(&g)

	cPrompt := C.CString(req.Prompt)
	defer C.free(unsafe.Pointer(cPrompt))
	g.prompt = cPrompt
	if req.Negative != "" {
		cNeg := C.CString(req.Negative)
		defer C.free(unsafe.Pointer(cNeg))
		g.negative_prompt = cNeg
	}
	if req.ClipSkip > 0 {
		g.clip_skip = C.int(req.ClipSkip)
	}
	if req.Width > 0 {
		g.width = C.int(req.Width)
	}
	if req.Height > 0 {
		g.height = C.int(req.Height)
	}
	if req.Steps > 0 {
		g.sample_params.sample_steps = C.int(req.Steps)
	}
	if req.CFG > 0 {
		g.sample_params.guidance.txt_cfg = C.float(req.CFG)
	}
	if req.Sampler != "" {
		cs := C.CString(req.Sampler)
		g.sample_params.sample_method = C.str_to_sample_method(cs)
		C.free(unsafe.Pointer(cs))
	}
	if req.Scheduler != "" {
		cs := C.CString(req.Scheduler)
		g.sample_params.scheduler = C.str_to_scheduler(cs)
		C.free(unsafe.Pointer(cs))
	}
	g.seed = C.int64_t(req.Seed)
	batch := req.Batch
	if batch < 1 {
		batch = 1
	}
	g.batch_count = C.int(batch)

	// hires.fix: start from sd.cpp's documented defaults, then enable + override
	// only when the request asks for it. sd.cpp derives the target size from
	// scale when target_width/height are 0, so setting scale is sufficient.
	C.sd_hires_params_init(&g.hires)
	if req.Hires {
		g.hires.enabled = C.bool(true)
		if req.HiresScale > 0 {
			g.hires.scale = C.float(req.HiresScale)
		}
		if req.HiresDenoise > 0 {
			g.hires.denoising_strength = C.float(req.HiresDenoise)
		}
		if req.HiresSteps > 0 {
			g.hires.steps = C.int(req.HiresSteps)
		}
		// str_to_sd_hires_upscaler matches sd.cpp's display names case-sensitively
		// ("Latent"/"Lanczos"/...), so map our lowercase names to the enum directly.
		g.hires.upscaler = hiresUpscalerEnum(req.HiresUpscaler)
		if strings.EqualFold(req.HiresUpscaler, "model") && req.HiresModel != "" {
			// model_path must outlive generate_image; the enclosing defer frees it.
			cHM := C.CString(req.HiresModel)
			defer C.free(unsafe.Pointer(cHM))
			g.hires.model_path = cHM
		}
	}

	// LoRAs: allocate the array in C memory. If it lived in a Go slice, passing
	// &g (whose g.loras would then be a Go pointer) to C would trip the cgo
	// pointer checker ("Go pointer to unpinned Go pointer").
	if n := len(req.LoRAs); n > 0 {
		arr := (*C.sd_lora_t)(C.malloc(C.size_t(n) * C.size_t(unsafe.Sizeof(C.sd_lora_t{}))))
		defer C.free(unsafe.Pointer(arr))
		slot := unsafe.Slice(arr, n)
		for i, l := range req.LoRAs {
			cPath := C.CString(l.Path)
			defer C.free(unsafe.Pointer(cPath))
			slot[i] = C.sd_lora_t{path: cPath, multiplier: C.float(l.Weight)}
		}
		g.loras = arr
		g.lora_count = C.uint32_t(n)
	}

	// img2img / inpaint: load the init image and match the output size to it.
	if req.InitImage != "" {
		ci, freeImg, err := loadInitImage(req.InitImage)
		if err != nil {
			return fmt.Errorf("sdcpp: init image: %w", err)
		}
		defer freeImg()
		g.init_image = ci
		g.width = C.int(ci.width)
		g.height = C.int(ci.height)
		if req.Strength > 0 {
			g.strength = C.float(req.Strength)
		}
		if req.Mask != "" {
			cm, freeMask, err := loadMaskImage(req.Mask)
			if err != nil {
				return fmt.Errorf("sdcpp: mask image: %w", err)
			}
			defer freeMask()
			if cm.width != ci.width || cm.height != ci.height {
				return fmt.Errorf("sdcpp: mask %dx%d must match the init image %dx%d",
					int(cm.width), int(cm.height), int(ci.width), int(ci.height))
			}
			g.mask_image = cm
		}
	} else if req.Mask != "" {
		return errors.New("sdcpp: --mask requires --init (inpaint edits an existing image)")
	}

	// ControlNet: load the guidance image, optionally run canny, and size the
	// output to it. Requires an OpenParams.ControlNet model at load time.
	if req.ControlImage != "" {
		cc, freeCtrl, err := loadInitImage(req.ControlImage)
		if err != nil {
			return fmt.Errorf("sdcpp: control image: %w", err)
		}
		defer freeCtrl()
		if req.Canny {
			C.preprocess_canny(cc, C.float(0.08), C.float(0.08), C.float(0.8), C.float(1.0), C.bool(false))
		}
		g.control_image = cc
		g.width = C.int(cc.width)
		g.height = C.int(cc.height)
		if req.ControlStrength > 0 {
			g.control_strength = C.float(req.ControlStrength)
		}
	}

	h := cgo.NewHandle(events)
	defer h.Delete()
	C.ifg_set_progress(unsafe.Pointer(h))
	defer C.ifg_silence_progress() // detach goProgress (LIFO: before h.Delete) and stay silent

	// Honor ctx: generate_image is a single blocking C call, so we wire ctx
	// cancellation into sd.cpp's real cancel (a flag the sampler checks) from a
	// watcher goroutine — otherwise an in-flight render runs to completion no
	// matter the ctx. Reset first, in case a prior render left a stale request.
	C.sd_cancel_generation(s.ctx, C.SD_CANCEL_RESET)
	watchDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			C.sd_cancel_generation(s.ctx, C.SD_CANCEL_ALL)
		case <-watchDone:
		}
	}()

	var imgs *C.sd_image_t
	var n C.int
	ok := bool(C.generate_image(s.ctx, &g, &imgs, &n))
	close(watchDone) // stop the watcher before reporting the result
	if err := ctx.Err(); err != nil {
		return err // cancelled: report context.Canceled, not a generic failure
	}
	if !ok || imgs == nil || n == 0 {
		return errors.New("sdcpp: generation failed")
	}
	defer C.free(unsafe.Pointer(imgs))

	list := unsafe.Slice(imgs, int(n))
	base := int64(g.seed) // sd.cpp uses request.seed + b for the b-th batch image
	for i := 0; i < int(n); i++ {
		seed := base + int64(i)
		path := outputPath(req.Output, i, int(n))
		var meta []PNGText
		if req.Metadata != nil {
			meta = req.Metadata(seed) // per-image metadata records this image's seed
		}
		err := saveImage(list[i], path, meta)
		C.free(unsafe.Pointer(list[i].data))
		if err != nil {
			return err
		}
		events <- Event{Kind: "done", Progress: 1, Output: path, Seed: seed}
	}
	return nil
}

// Samplers returns the sampler names sd.cpp accepts; Schedulers the schedulers.
// Reflected from sd.cpp (not hard-coded) so they never drift from the runtime.
// Used for validation error messages and CLI help.
func Samplers() []string {
	n := int(C.SAMPLE_METHOD_COUNT)
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, C.GoString(C.sd_sample_method_name(C.enum_sample_method_t(i))))
	}
	return out
}

// Schedulers returns the scheduler names sd.cpp accepts (plus the "normal" alias
// for "discrete", which str_to_scheduler also accepts but sd_scheduler_name omits).
func Schedulers() []string {
	n := int(C.SCHEDULER_COUNT)
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, C.GoString(C.sd_scheduler_name(C.enum_scheduler_t(i))))
	}
	return out
}

// ValidSampler / ValidScheduler mirror sd.cpp's own str_to_* acceptance exactly
// (an unknown name returns the out-of-range COUNT sentinel), so a typo is caught
// with a clear error instead of silently producing undefined output.
func ValidSampler(name string) bool {
	cs := C.CString(name)
	defer C.free(unsafe.Pointer(cs))
	return C.str_to_sample_method(cs) != C.SAMPLE_METHOD_COUNT
}

func ValidScheduler(name string) bool {
	cs := C.CString(name)
	defer C.free(unsafe.Pointer(cs))
	return C.str_to_scheduler(cs) != C.SCHEDULER_COUNT
}

// hiresUpscalerEnum maps image-forge's lowercase upscaler names to the sd.cpp
// enum. sd.cpp's str_to_sd_hires_upscaler only matches its display names
// ("Latent", "Lanczos", ...) with a case-sensitive strcmp, so it cannot be used
// for lowercase input — an unrecognized value returns COUNT and sd.cpp disables
// hires. Anything unknown (or empty) falls back to the no-download latent path.
func hiresUpscalerEnum(name string) C.enum_sd_hires_upscaler_t {
	switch strings.ToLower(name) {
	case "lanczos":
		return C.SD_HIRES_UPSCALER_LANCZOS
	case "nearest":
		return C.SD_HIRES_UPSCALER_NEAREST
	case "model":
		return C.SD_HIRES_UPSCALER_MODEL
	default: // "" or "latent"
		return C.SD_HIRES_UPSCALER_LATENT
	}
}

// Upscale runs a standalone Real-ESRGAN super-resolution pass over a single
// image. The ESRGAN model's own scale (typically 4x) governs the output size;
// the upscale_factor argument is accepted by the C API but ignored for the
// Real-ESRGAN models, so p.Factor is passed through only for API completeness.
func Upscale(p UpscaleParams) error {
	if p.ESRGANPath == "" {
		return errors.New("sdcpp: an ESRGAN model path is required")
	}
	cEsrgan := C.CString(p.ESRGANPath)
	defer C.free(unsafe.Pointer(cEsrgan))

	// Keep sd.cpp's progress printer silenced during the model load (it would
	// otherwise printf to stdout and corrupt machine consumers such as MCP).
	C.ifg_silence_progress()

	// direct=false and n_threads=physical-cores match how new_sd_ctx is configured
	// (sd_ctx_params_init defaults n_threads to sd_get_num_physical_cores) and how
	// sd.cpp builds its own hires model upscaler. tile_size 512 keeps peak memory
	// in check on the 16 GB baseline (a 512 tile -> ~2048 px at 4x); NULL backend
	// selects the default Metal backend.
	ctx := C.new_upscaler_ctx(cEsrgan, C.bool(false), C.sd_get_num_physical_cores(), C.int(512), nil, nil)
	if ctx == nil {
		return fmt.Errorf("sdcpp: failed to load upscaler model %q", p.ESRGANPath)
	}
	defer C.free_upscaler_ctx(ctx)

	ci, freeImg, err := loadInitImage(p.InputPath)
	if err != nil {
		return fmt.Errorf("sdcpp: upscale input: %w", err)
	}
	defer freeImg()

	factor := p.Factor
	if factor < 1 {
		factor = int(C.get_upscale_factor(ctx))
		if factor < 1 {
			factor = 4
		}
	}

	// Route any progress the upscaler reports to Events, then restore silence.
	if p.Events != nil {
		h := cgo.NewHandle(p.Events)
		defer h.Delete()
		C.ifg_set_progress(unsafe.Pointer(h))
		defer C.ifg_silence_progress()
	}

	var out *C.sd_image_t
	var n C.int
	if !bool(C.upscale(ctx, ci, C.uint32_t(factor), &out, &n)) || out == nil || n == 0 {
		return errors.New("sdcpp: upscale failed")
	}
	defer C.free(unsafe.Pointer(out))

	list := unsafe.Slice(out, int(n))
	saveErr := saveImage(list[0], p.OutputPath, p.Metadata)
	for i := 0; i < int(n); i++ {
		C.free(unsafe.Pointer(list[i].data))
	}
	if saveErr != nil {
		return saveErr
	}
	if p.Events != nil {
		p.Events <- Event{Kind: "done", Progress: 1, Output: p.OutputPath}
	}
	return nil
}

// loadInitImage decodes a PNG/JPEG file into an RGB sd_image_t backed by C
// memory. The returned func frees that memory; call it after generate_image.
func loadInitImage(path string) (C.sd_image_t, func(), error) {
	f, err := os.Open(path)
	if err != nil {
		return C.sd_image_t{}, func() {}, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return C.sd_image_t{}, func() {}, err
	}
	b := img.Bounds()
	w, hgt := b.Dx(), b.Dy()
	buf := C.malloc(C.size_t(w * hgt * 3))
	if buf == nil {
		return C.sd_image_t{}, func() {}, errors.New("out of memory")
	}
	pix := unsafe.Slice((*byte)(buf), w*hgt*3)
	i := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			pix[i], pix[i+1], pix[i+2] = byte(r>>8), byte(g>>8), byte(bl>>8)
			i += 3
		}
	}
	ci := C.sd_image_t{
		width:   C.uint32_t(w),
		height:  C.uint32_t(hgt),
		channel: 3,
		data:    (*C.uint8_t)(buf),
	}
	return ci, func() { C.free(buf) }, nil
}

// loadMaskImage decodes a PNG/JPEG mask into a 1-channel sd_image_t (white =
// regenerate, black = keep), backed by C memory freed via the returned func.
func loadMaskImage(path string) (C.sd_image_t, func(), error) {
	f, err := os.Open(path)
	if err != nil {
		return C.sd_image_t{}, func() {}, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return C.sd_image_t{}, func() {}, err
	}
	b := img.Bounds()
	w, hgt := b.Dx(), b.Dy()
	buf := C.malloc(C.size_t(w * hgt))
	if buf == nil {
		return C.sd_image_t{}, func() {}, errors.New("out of memory")
	}
	pix := unsafe.Slice((*byte)(buf), w*hgt)
	i := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, _, _, _ := img.At(x, y).RGBA()
			pix[i] = byte(r >> 8)
			i++
		}
	}
	ci := C.sd_image_t{width: C.uint32_t(w), height: C.uint32_t(hgt), channel: 1, data: (*C.uint8_t)(buf)}
	return ci, func() { C.free(buf) }, nil
}

// saveImage encodes an sd_image_t (RGB/RGBA/gray) to a PNG file, splicing the
// given generation-metadata text chunks in after IHDR (see encodePNGWithText).
func saveImage(ci C.sd_image_t, path string, texts []PNGText) error {
	w, hgt, ch := int(ci.width), int(ci.height), int(ci.channel)
	if ci.data == nil || w == 0 || hgt == 0 {
		return errors.New("sdcpp: empty image returned")
	}
	raw := C.GoBytes(unsafe.Pointer(ci.data), C.int(w*hgt*ch))
	img := image.NewNRGBA(image.Rect(0, 0, w, hgt))
	for y := 0; y < hgt; y++ {
		for x := 0; x < w; x++ {
			si, di := (y*w+x)*ch, img.PixOffset(x, y)
			switch ch {
			case 3:
				img.Pix[di], img.Pix[di+1], img.Pix[di+2], img.Pix[di+3] = raw[si], raw[si+1], raw[si+2], 255
			case 4:
				copy(img.Pix[di:di+4], raw[si:si+4])
			case 1:
				img.Pix[di], img.Pix[di+1], img.Pix[di+2], img.Pix[di+3] = raw[si], raw[si], raw[si], 255
			default:
				return fmt.Errorf("sdcpp: unsupported channel count %d", ch)
			}
		}
	}
	data, err := encodePNGWithText(img, texts)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
