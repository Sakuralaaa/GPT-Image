package protocol

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color/palette"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"chatgpt2api/internal/service"
)

func testGIFBytes(t *testing.T, frames int) []byte {
	t.Helper()
	anim := &gif.GIF{}
	for i := 0; i < frames; i++ {
		img := image.NewPaletted(image.Rect(0, 0, 4, 4), palette.Plan9)
		img.SetColorIndex(i%4, 0, uint8(i+1))
		anim.Image = append(anim.Image, img)
		anim.Delay = append(anim.Delay, 10)
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, anim); err != nil {
		t.Fatalf("gif.EncodeAll() error = %v", err)
	}
	return buf.Bytes()
}

func TestConversationRequestNormalizedGIFFrames(t *testing.T) {
	gifRequest := ConversationRequest{OutputFormat: "gif", FrameCount: 99}.Normalized()
	if gifRequest.OutputFormat != "gif" || gifRequest.FrameCount != service.MaxGIFFrameCount || gifRequest.FrameDurationMS != service.DefaultGIFFrameDurationMS {
		t.Fatalf("gif request = %+v", gifRequest)
	}
	pngRequest := ConversationRequest{OutputFormat: "png", FrameCount: 5, FrameDurationMS: 50}.Normalized()
	if pngRequest.FrameCount != 0 || pngRequest.FrameDurationMS != 0 {
		t.Fatalf("png request kept frames: %+v", pngRequest)
	}
}

func TestFormatImageResultKeepsGIFBytes(t *testing.T) {
	config := testProtocolImageConfig{root: t.TempDir()}
	engine := &Engine{Config: config}
	gifData := testGIFBytes(t, 3)

	result := engine.FormatImageResultWithOptions(
		[]map[string]any{{"b64_json": base64.StdEncoding.EncodeToString(gifData)}},
		"draw", "url", "https://example.test", "owner-1", "Alice", 123, "",
		ImageOutputOptions{Format: "gif", FrameCount: 3, FrameDurationMS: 100},
	)
	items, _ := result["data"].([]map[string]any)
	if len(items) != 1 || items[0]["output_format"] != "gif" {
		t.Fatalf("data = %#v", result["data"])
	}
	imageURL, _ := items[0]["url"].(string)
	if !strings.HasSuffix(imageURL, ".gif") {
		t.Fatalf("image url = %q, want .gif", imageURL)
	}
	rel := strings.TrimPrefix(imageURL, "https://example.test/images/")
	stored, err := os.ReadFile(filepath.Join(config.ImagesDir(), filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !bytes.Equal(stored, gifData) || service.GIFFrameCountOf(stored) != 3 {
		t.Fatal("stored GIF was re-encoded")
	}
}

func TestFormatImageResultGIFRequestWithStillImage(t *testing.T) {
	config := testProtocolImageConfig{root: t.TempDir()}
	engine := &Engine{Config: config}
	pngData := encodeTestPNGForGIF(t)

	result := engine.FormatImageResultWithOptions(
		[]map[string]any{{"b64_json": base64.StdEncoding.EncodeToString(pngData)}},
		"draw", "url", "https://example.test", "owner-1", "Alice", 123, "",
		ImageOutputOptions{Format: "gif"},
	)
	items, _ := result["data"].([]map[string]any)
	if len(items) != 1 || items[0]["output_format"] != "png" {
		t.Fatalf("data = %#v", result["data"])
	}
	imageURL, _ := items[0]["url"].(string)
	if !strings.HasSuffix(imageURL, ".png") {
		t.Fatalf("image url = %q, want .png", imageURL)
	}
}

func TestResponseImageGenerationRequestGIF(t *testing.T) {
	body := map[string]any{
		"model": "gpt-5.5",
		"input": "draw a cat",
		"tools": []any{map[string]any{"type": "image_generation", "output_format": "gif", "frame_count": 6, "frame_duration_ms": 200}},
	}
	request, _, err := ResponseImageGenerationRequest(body, "", nil)
	if err != nil {
		t.Fatalf("ResponseImageGenerationRequest() error = %v", err)
	}
	if request.OutputFormat != "gif" || request.FrameCount != 6 || request.FrameDurationMS != 200 {
		t.Fatalf("request = %+v", request)
	}
	body["stream"] = true
	if _, _, err := ResponseImageGenerationRequest(body, "", nil); err == nil {
		t.Fatal("gif + stream accepted")
	}
}

func encodeTestPNGForGIF(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode() error = %v", err)
	}
	return buf.Bytes()
}
