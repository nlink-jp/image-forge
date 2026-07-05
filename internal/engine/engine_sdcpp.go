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
	"unsafe"
)

// Info returns build/system info from the linked stable-diffusion.cpp runtime.
func Info() string {
	return "engine: stable-diffusion.cpp — " + C.GoString(C.sd_get_system_info())
}

// New returns the sd.cpp-backed engine.
func New() (Engine, error) { return &sdEngine{}, nil }

type sdEngine struct{}

func (e *sdEngine) Close() error { return nil }

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

// Generate renders a txt2img request. The caller must read from events
// concurrently (it is used both for load/done and for step progress).
func (e *sdEngine) Generate(ctx context.Context, req Request, events chan<- Event) error {
	if req.ModelPath == "" {
		return errors.New("sdcpp: a model path is required (-model)")
	}

	events <- Event{Kind: "load", Message: "loading model"}

	var cp C.sd_ctx_params_t
	C.sd_ctx_params_init(&cp)
	cModel := C.CString(req.ModelPath)
	defer C.free(unsafe.Pointer(cModel))
	cp.model_path = cModel
	if req.VAEPath != "" {
		cVAE := C.CString(req.VAEPath)
		defer C.free(unsafe.Pointer(cVAE))
		cp.vae_path = cVAE
	}

	sdCtx := C.new_sd_ctx(&cp)
	if sdCtx == nil {
		return fmt.Errorf("sdcpp: failed to load model %q", req.ModelPath)
	}
	defer C.free_sd_ctx(sdCtx)

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

	// LoRAs: build a C array. The struct fields are C pointers, so passing the
	// Go backing array to C is allowed; keep the CStrings alive across the call.
	var cloras []C.sd_lora_t
	for _, l := range req.LoRAs {
		cPath := C.CString(l.Path)
		defer C.free(unsafe.Pointer(cPath))
		cloras = append(cloras, C.sd_lora_t{path: cPath, multiplier: C.float(l.Weight)})
	}
	if len(cloras) > 0 {
		g.loras = &cloras[0]
		g.lora_count = C.uint32_t(len(cloras))
	}

	// img2img: load the init image and match the output size to it.
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
	}

	h := cgo.NewHandle(events)
	defer h.Delete()
	C.ifg_set_progress(unsafe.Pointer(h))
	defer C.ifg_clear_progress()

	var imgs *C.sd_image_t
	var n C.int
	if !bool(C.generate_image(sdCtx, &g, &imgs, &n)) || imgs == nil || n == 0 {
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

// loadInitImage decodes a PNG/JPEG file into an RGB sd_image_t backed by
// C memory. The returned func frees that memory; call it after generate_image.
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
