package engine

import "testing"

func TestOutputPath(t *testing.T) {
	cases := []struct {
		base       string
		i, total   int
		want       string
	}{
		{"out.png", 0, 1, "out.png"},           // single image: unchanged
		{"", 0, 1, "out.png"},                   // default name
		{"a/b/pic.png", 0, 3, "a/b/pic-0.png"}, // batch: index before ext
		{"a/b/pic.png", 2, 3, "a/b/pic-2.png"},
		{"noext", 1, 2, "noext-1.png"}, // missing ext defaults to .png
	}
	for _, c := range cases {
		if got := outputPath(c.base, c.i, c.total); got != c.want {
			t.Errorf("outputPath(%q,%d,%d) = %q, want %q", c.base, c.i, c.total, got, c.want)
		}
	}
}
