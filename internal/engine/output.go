package engine

import (
	"fmt"
	"path/filepath"
	"strings"
)

// outputPath returns the file path for image i of a batch of `total`. For a
// single image it returns base unchanged; for batches it inserts the index
// before the extension (out.png -> out-0.png, out-1.png, ...).
func outputPath(base string, i, total int) string {
	if base == "" {
		base = "out.png"
	}
	if total <= 1 {
		return base
	}
	ext := filepath.Ext(base)
	if ext == "" {
		ext = ".png"
	}
	return fmt.Sprintf("%s-%d%s", strings.TrimSuffix(base, ext), i, ext)
}
