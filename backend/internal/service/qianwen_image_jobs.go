package service

import (
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
)

const QianwenNativeText2ImagePath = "/api/v1/services/aigc/text2image/image-synthesis"

// Official Beijing per-image prices, checked 2026-09-22:
// https://help.aliyun.com/zh/model-studio/model-pricing (万相文生图).
// wan2.6-t2i uses the newer image-generation endpoint and is out of this
// adapter's scope; z-image-turbo lacks endpoint evidence on the text-to-image
// API page and stays unverified.
var qianwenText2ImageTariffs = map[string]struct {
	unitCNY       float64
	maxPromptRuns int
}{
	"wan2.5-t2i-preview": {0.20, 2000},
	"wan2.2-t2i-plus":    {0.20, 500},
	"wan2.2-t2i-flash":   {0.14, 500},
	"wanx2.1-t2i-plus":   {0.20, 500},
	"wanx2.1-t2i-turbo":  {0.14, 500},
	"wanx2.0-t2i-turbo":  {0.04, 800},
}

// qianwenImageAsyncSpecification validates the documented old-protocol
// text-to-image request and returns its Beijing tariff. Billing is per
// successfully generated image; the settled quantity comes from the upstream
// usage.image_count evidence, never from the requested n.
func qianwenImageAsyncSpecification(model string, r gjson.Result) (*QianwenAsyncSnapshot, error) {
	bad := func() (*QianwenAsyncSnapshot, error) {
		return nil, fmt.Errorf("unsupported image model, input or pricing specification")
	}
	tariff, ok := qianwenText2ImageTariffs[model]
	if !ok {
		return bad()
	}
	prompt := r.Get("input.prompt")
	if prompt.Type != gjson.String || strings.TrimSpace(prompt.String()) == "" || len([]rune(prompt.String())) > tariff.maxPromptRuns {
		return bad()
	}
	if negative := r.Get("input.negative_prompt"); negative.Exists() && (negative.Type != gjson.String || len([]rune(negative.String())) > 500) {
		return bad()
	}
	unknownInput := false
	r.Get("input").ForEach(func(k, _ gjson.Result) bool {
		if k.String() != "prompt" && k.String() != "negative_prompt" {
			unknownInput = true
		}
		return true
	})
	if unknownInput {
		return bad()
	}
	if size := r.Get("parameters.size"); size.Exists() {
		if !validQianwenText2ImageSize(model, size.String()) {
			return bad()
		}
	}
	if n := r.Get("parameters.n"); n.Exists() && (n.Type != gjson.Number || n.Float() != float64(n.Int()) || n.Int() < 1 || n.Int() > 4) {
		return bad()
	}
	if seed := r.Get("parameters.seed"); seed.Exists() && (seed.Type != gjson.Number || seed.Float() != float64(seed.Int()) || seed.Int() < 0 || seed.Int() > 2147483647) {
		return bad()
	}
	for _, field := range []string{"prompt_extend", "watermark"} {
		if v := r.Get("parameters." + field); v.Exists() && v.Type != gjson.True && v.Type != gjson.False {
			return bad()
		}
	}
	unknown := false
	r.Get("parameters").ForEach(func(k, _ gjson.Result) bool {
		switch k.String() {
		case "size", "n", "prompt_extend", "watermark", "seed":
		default:
			unknown = true
		}
		return true
	})
	if unknown {
		return bad()
	}
	return &QianwenAsyncSnapshot{UnitCNY: tariff.unitCNY, TaskType: "text-to-image"}, nil
}

// validQianwenText2ImageSize enforces the documented bounds: sides 512–1440,
// aspect ratio between 1:4 and 4:1; wan2.5-t2i-preview additionally constrains
// the total pixel area to the documented 1280*1280..1440*1440 band.
func validQianwenText2ImageSize(model, size string) bool {
	parts := strings.Split(size, "*")
	if len(parts) != 2 {
		return false
	}
	var width, height int
	if _, err := fmt.Sscanf(parts[0], "%d", &width); err != nil {
		return false
	}
	if _, err := fmt.Sscanf(parts[1], "%d", &height); err != nil {
		return false
	}
	if width < 512 || width > 1440 || height < 512 || height > 1440 {
		return false
	}
	if width > height*4 || height > width*4 {
		return false
	}
	if model == "wan2.5-t2i-preview" {
		area := width * height
		if area < 1280*1280 || area > 1440*1440 {
			return false
		}
	}
	return true
}
