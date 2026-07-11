package cli

import (
	"fmt"
	"strings"

	"github.com/nlink-jp/image-forge/internal/engine"
)

// validateSamplerScheduler rejects an unknown sampler/scheduler name up front
// (issue #3). sd.cpp's str_to_sample_method / str_to_scheduler return an
// out-of-range sentinel for a typo (e.g. `--sampler eluer_a`), which then
// produces undefined output instead of a clear error. Empty means "use the
// default". Shared by gen and the serve/MCP render path (buildRender).
func validateSamplerScheduler(sampler, scheduler string) error {
	if sampler != "" && !engine.ValidSampler(sampler) {
		return fmt.Errorf("invalid sampler %q (valid: %s)", sampler, strings.Join(engine.Samplers(), ", "))
	}
	if scheduler != "" && !engine.ValidScheduler(scheduler) {
		valid := engine.Schedulers()
		if len(valid) > 0 {
			valid = append(valid, "normal")
		}
		return fmt.Errorf("invalid scheduler %q (valid: %s)", scheduler, strings.Join(valid, ", "))
	}
	return nil
}

// validateWType rejects an unknown load-time weight-quantization type up front
// (empty = keep the checkpoint's original weights). Shared by gen and the
// serve/MCP render path.
func validateWType(wtype string) error {
	if wtype != "" && !engine.ValidWType(wtype) {
		return fmt.Errorf("invalid wtype %q (valid: %s)", wtype, strings.Join(engine.QuantTypes(), ", "))
	}
	return nil
}
