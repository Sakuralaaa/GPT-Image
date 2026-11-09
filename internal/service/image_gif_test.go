package service

import (
	"bytes"
	"image"
	"image/color"
	"image/color/palette"
	"image/gif"
	"image/png"
	"testing"
)

func encodeTestGIF(t *testing.T, frames int) []byte {
	t.Helper()
	anim := &gif.GIF{}
	for i := 0; i < frames; i++ {
		img := image.NewPaletted(image.Rect(0, 0, 4, 4), palette.Plan9)
		img.SetColorIndex(i%4, i%4, uint8(i+1))
		anim.Image = append(anim.Image, img)
		anim.Delay = append(anim.Delay, 12)
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, anim); err != nil {
		t.Fatalf("gif.EncodeAll() error = %v", err)
	}
	return buf.Bytes()
}

func encodeTestPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.NRGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode() error = %v", err)
	}
	return buf.Bytes()
}

func TestIsGIFBytesAndSniffImageFormat(t *testing.T) {
	gifData := encodeTestGIF(t, 3)
	if !IsGIFBytes(gifData) {
		t.Fatal("IsGIFBytes(gif) = false")
	}
	if IsGIFBytes([]byte("GIF8")) {
		t.Fatal("IsGIFBytes(short) = true")
	}
	cases := map[string][]byte{
		"gif":  gifData,
		"png":  encodeTestPNG(t),
		"jpeg": {0xFF, 0xD8, 0xFF, 0xE0, 0x00},
		"webp": []byte("RIFF\x00\x00\x00\x00WEBPVP8 "),
		"":     []byte("not an image"),
	}
	for want, data := range cases {
		if got := SniffImageFormat(data); got != want {
			t.Fatalf("SniffImageFormat(%q) = %q, want %q", want, got, want)
		}
	}
}

func TestGIFFrameCountOf(t *testing.T) {
	for _, frames := range []int{1, 3, 8} {
		if got := GIFFrameCountOf(encodeTestGIF(t, frames)); got != frames {
			t.Fatalf("GIFFrameCountOf(%d frames) = %d", frames, got)
		}
	}
	if got := GIFFrameCountOf(encodeTestPNG(t)); got != 0 {
		t.Fatalf("GIFFrameCountOf(png) = %d, want 0", got)
	}
	data := encodeTestGIF(t, 3)
	if got := GIFFrameCountOf(data[:len(data)/2]); got > 3 {
		t.Fatalf("GIFFrameCountOf(truncated) = %d", got)
	}
}

func TestNormalizeGIFFrameOptions(t *testing.T) {
	cases := []struct {
		count, duration         int
		wantCount, wantDuration int
	}{
		{0, 0, DefaultGIFFrameCount, DefaultGIFFrameDurationMS},
		{1, 5, MinGIFFrameCount, MinGIFFrameDurationMS},
		{100, 9000, MaxGIFFrameCount, MaxGIFFrameDurationMS},
		{12, 250, 12, 250},
		{-3, -1, DefaultGIFFrameCount, DefaultGIFFrameDurationMS},
	}
	for _, tc := range cases {
		gotCount, gotDuration := NormalizeGIFFrameOptions(tc.count, tc.duration)
		if gotCount != tc.wantCount || gotDuration != tc.wantDuration {
			t.Fatalf("NormalizeGIFFrameOptions(%d, %d) = (%d, %d), want (%d, %d)", tc.count, tc.duration, gotCount, gotDuration, tc.wantCount, tc.wantDuration)
		}
	}
}

func TestGIFFrameOptionsFromBody(t *testing.T) {
	count, duration := GIFFrameOptionsFromBody(map[string]any{"output_format": "png", "frame_count": 10})
	if count != 0 || duration != 0 {
		t.Fatalf("non-gif = (%d, %d), want zeros", count, duration)
	}
	count, duration = GIFFrameOptionsFromBody(map[string]any{"output_format": "GIF", "frame_count": "10", "frame_duration_ms": 80})
	if count != 10 || duration != 80 {
		t.Fatalf("gif = (%d, %d), want (10, 80)", count, duration)
	}
	count, duration = GIFFrameOptionsFromBody(map[string]any{"output_format": "gif"})
	if count != DefaultGIFFrameCount || duration != DefaultGIFFrameDurationMS {
		t.Fatalf("gif defaults = (%d, %d)", count, duration)
	}
}

func TestAppendGIFPromptInstruction(t *testing.T) {
	if got := AppendGIFPromptInstruction("  ", 4, 100); got != "" {
		t.Fatalf("empty prompt = %q", got)
	}
	got := AppendGIFPromptInstruction("a cat", 4, 100)
	want := "a cat\n\n" + GIFPromptInstruction(4, 100)
	if got != want {
		t.Fatalf("AppendGIFPromptInstruction() = %q, want %q", got, want)
	}
}

func TestValidateGIFStreamRequest(t *testing.T) {
	if err := ValidateGIFStreamRequest(map[string]any{"output_format": "gif", "stream": true}); err == nil {
		t.Fatal("gif + stream accepted")
	}
	if err := ValidateGIFStreamRequest(map[string]any{"output_format": "gif"}); err != nil {
		t.Fatalf("gif without stream rejected: %v", err)
	}
	if err := ValidateGIFStreamRequest(map[string]any{"output_format": "png", "stream": true}); err != nil {
		t.Fatalf("png + stream rejected: %v", err)
	}
}

func TestMergeImageOutputOptionsGIFFrames(t *testing.T) {
	payload := map[string]any{"frame_count": 3}
	mergeImageOutputOptions(payload, ImageOutputOptions{Format: "gif", FrameCount: 30})
	if payload["output_format"] != "gif" || payload["frame_count"] != MaxGIFFrameCount || payload["frame_duration_ms"] != DefaultGIFFrameDurationMS {
		t.Fatalf("gif payload = %#v", payload)
	}
	mergeImageOutputOptions(payload, ImageOutputOptions{Format: "png"})
	if _, ok := payload["frame_count"]; ok {
		t.Fatalf("png payload kept frame_count: %#v", payload)
	}
}
