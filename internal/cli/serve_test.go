package cli

import (
	"encoding/json"
	"testing"
)

// A serve request line's guidance knobs decode from their JSON keys and map onto
// the shared RenderRequest, so `serve` and `mcp` reach the same engine params.
func TestServeRequestGuidanceMapping(t *testing.T) {
	line := `{"prompt":"x","model":"flux1-dev","guidance":3.5,"flow_shift":3.0,"slg_scale":2.5,"img_cfg":1.5}`
	var sr serveRequest
	if err := json.Unmarshal([]byte(line), &sr); err != nil {
		t.Fatal(err)
	}
	rr := sr.renderRequest()
	for _, tc := range []struct {
		name string
		got  *float64
		want float64
	}{
		{"guidance", rr.Guidance, 3.5},
		{"flow_shift", rr.FlowShift, 3.0},
		{"slg_scale", rr.SLGScale, 2.5},
		{"img_cfg", rr.ImgCFG, 1.5},
	} {
		if tc.got == nil || *tc.got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, tc.got, tc.want)
		}
	}
}

// Omitted guidance keys stay nil so the engine keeps sd.cpp's defaults.
func TestServeRequestGuidanceOmittedIsNil(t *testing.T) {
	var sr serveRequest
	if err := json.Unmarshal([]byte(`{"prompt":"x"}`), &sr); err != nil {
		t.Fatal(err)
	}
	rr := sr.renderRequest()
	if rr.Guidance != nil || rr.FlowShift != nil || rr.SLGScale != nil || rr.ImgCFG != nil {
		t.Error("omitted guidance knobs should decode to nil (keep sd.cpp defaults)")
	}
}
