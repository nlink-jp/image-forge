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
static void ifg_clear_progress(void)     { sd_set_progress_callback(NULL, NULL); }
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // register JPEG decoder for init images
	"image/png"
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
func Open(modelPath, vaePath string) (Session, error) {
	if modelPath == "" {
		return nil, errors.New("sdcpp: a model path is required")
	}
	var cp C.sd_ctx_params_t
	C.sd_ctx_params_init(&cp)
	cModel := C.CString(modelPath)
	defer C.free(unsafe.Pointer(cModel))
	cp.model_path = cModel
	if vaePath != "" {
		cVAE := C.CString(vaePath)
		defer C.free(unsafe.Pointer(cVAE))
		cp.vae_path = cVAE
	}
	ctx := C.new_sd_ctx(&cp)
	if ctx == nil {
		return nil, fmt.Errorf("sdcpp: failed to load model %q", modelPath)
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
	g.seed = C.int64_t(req.Seed)
	batch := req.Batch
	if batch < 1 {
		batch = 1
	}
	g.batch_count = C.int(batch)

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

	h := cgo.NewHandle(events)
	defer h.Delete()
	C.ifg_set_progress(unsafe.Pointer(h))
	defer C.ifg_clear_progress()

	var imgs *C.sd_image_t
	var n C.int
	if !bool(C.generate_image(s.ctx, &g, &imgs, &n)) || imgs == nil || n == 0 {
		return errors.New("sdcpp: generation failed")
	}
	defer C.free(unsafe.Pointer(imgs))

	list := unsafe.Slice(imgs, int(n))
	for i := 0; i < int(n); i++ {
		path := outputPath(req.Output, i, int(n))
		err := saveImage(list[i], path)
		C.free(unsafe.Pointer(list[i].data))
		if err != nil {
			return err
		}
		events <- Event{Kind: "done", Progress: 1, Output: path}
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

// saveImage encodes an sd_image_t (RGB/RGBA/gray) to a PNG file.
func saveImage(ci C.sd_image_t, path string) error {
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
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}
