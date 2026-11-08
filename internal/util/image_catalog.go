package util

import (
	"strings"
	"sync"
)

// ImageModelSpec is one row of the client-facing image model table.
//
// The ChatGPT web conversation path has no per-request image model: upstream
// picks the image engine (Images 2.6 since 2026-11-04). What a client model
// controls is the driving chat model sent as "model" on
// /backend-api/f/conversation. Aliases resolve to a canonical row first.
type ImageModelSpec struct {
	ID              string `json:"id"`
	Label           string `json:"label"`
	AliasOf         string `json:"alias_of,omitempty"`
	DefaultUpstream string `json:"default_upstream,omitempty"`
	Description     string `json:"description,omitempty"`
}

// ImageModelCatalog is the single image model table. Add new client models
// here; ModelIDs/ImageGenerationModelIDs list the ids in /v1/models order.
var ImageModelCatalog = []ImageModelSpec{
	{ID: ImageModelGPT26, Label: "GPT Image 2.6", DefaultUpstream: "auto", Description: "上游自动选择"},
	{ID: ImageModelGPT26Flare, Label: "GPT Image 2.6 Flare", DefaultUpstream: "auto", Description: "可改为即时模型驱动（如 gpt-6）"},
	{ID: ImageModelGPT26Sunburst, Label: "GPT Image 2.6 Sunburst", DefaultUpstream: "auto", Description: "可改为思考模型驱动（如 gpt-6-thinking）"},
	{ID: ImageModelAuto, Label: "Auto", AliasOf: ImageModelGPT26},
	{ID: ImageModelGPT, Label: "gpt-image-2 (兼容)", AliasOf: ImageModelGPT26},
	{ID: ImageModelGPT25, Label: "gpt-image-2.5 (兼容)", AliasOf: ImageModelGPT26},
	{ID: ImageModelGPT25Flare, Label: "gpt-image-2.5-flare (兼容)", AliasOf: ImageModelGPT26Flare},
	{ID: ImageModelGPT25Sunburst, Label: "gpt-image-2.5-sunburst (兼容)", AliasOf: ImageModelGPT26Sunburst},
	{ID: ImageModelCodex, Label: "codex-gpt-image-2 (兼容)", AliasOf: ImageModelGPT26},
}

var (
	imageUpstreamMu        sync.RWMutex
	imageUpstreamOverrides = map[string]string{}
)

func imageModelSpec(id string) (ImageModelSpec, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, spec := range ImageModelCatalog {
		if spec.ID == id {
			return spec, true
		}
	}
	return ImageModelSpec{}, false
}

// CanonicalImageModel resolves aliases to their canonical catalog id. Unknown
// models resolve to "".
func CanonicalImageModel(model string) string {
	spec, ok := imageModelSpec(model)
	if strings.TrimSpace(model) == "" {
		spec, ok = imageModelSpec(ImageModelAuto)
	}
	for depth := 0; ok && spec.AliasOf != "" && depth < 4; depth++ {
		spec, ok = imageModelSpec(spec.AliasOf)
	}
	if !ok {
		return ""
	}
	return spec.ID
}

// SetImageUpstreamOverrides installs admin overrides (canonical id -> driving
// model). Unknown ids and empty values are ignored.
func SetImageUpstreamOverrides(overrides map[string]string) {
	next := map[string]string{}
	for id, upstream := range overrides {
		canonical := CanonicalImageModel(id)
		upstream = strings.TrimSpace(upstream)
		if canonical == "" || upstream == "" {
			continue
		}
		next[canonical] = upstream
	}
	imageUpstreamMu.Lock()
	imageUpstreamOverrides = next
	imageUpstreamMu.Unlock()
}

// ImageUpstreamModel returns the driving chat model for a client image model.
// Non-image or unknown models drive with "auto".
func ImageUpstreamModel(model string) string {
	canonical := CanonicalImageModel(model)
	if canonical == "" {
		return ImageModelAuto
	}
	imageUpstreamMu.RLock()
	override := imageUpstreamOverrides[canonical]
	imageUpstreamMu.RUnlock()
	if override != "" {
		return override
	}
	spec, _ := imageModelSpec(canonical)
	if spec.DefaultUpstream == "" {
		return ImageModelAuto
	}
	return spec.DefaultUpstream
}

// ImageUpstreamTable returns the effective canonical id -> driving model map.
func ImageUpstreamTable() map[string]string {
	out := map[string]string{}
	for _, spec := range ImageModelCatalog {
		if spec.AliasOf == "" {
			out[spec.ID] = ImageUpstreamModel(spec.ID)
		}
	}
	return out
}

// ImageUpstreamDefaults returns the built-in canonical id -> driving model map.
func ImageUpstreamDefaults() map[string]string {
	out := map[string]string{}
	for _, spec := range ImageModelCatalog {
		if spec.AliasOf == "" {
			out[spec.ID] = spec.DefaultUpstream
		}
	}
	return out
}
