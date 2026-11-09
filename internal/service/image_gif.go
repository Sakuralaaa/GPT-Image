package service

import (
	"bytes"
	"fmt"
	"strings"

	"chatgpt2api/internal/util"
)

// Animated GIF output. Upstream image tools have no GIF parameter, so a GIF
// request keeps the upstream model slug and output format untouched and only
// appends an instruction to the prompt sent upstream. Results that really are
// GIF bytes are stored as-is (no re-encoding, which would drop frames);
// anything else is stored in its actual format.
const (
	ImageOutputFormatGIF       = "gif"
	DefaultGIFFrameCount       = 8
	MinGIFFrameCount           = 2
	MaxGIFFrameCount           = 24
	DefaultGIFFrameDurationMS  = 120
	MinGIFFrameDurationMS      = 20
	MaxGIFFrameDurationMS      = 2000
	gifPromptInstructionFormat = "输出为动画 GIF，共 %d 帧，每帧 %d 毫秒，循环播放；各帧保持同一主体与构图，只表现连续的动作变化。"
)

// IsGIFOutputFormat reports whether the requested format is GIF.
func IsGIFOutputFormat(format string) bool {
	return strings.EqualFold(strings.TrimSpace(format), ImageOutputFormatGIF)
}

// IsGIFBytes checks the GIF87a / GIF89a magic.
func IsGIFBytes(data []byte) bool {
	return len(data) >= 6 && (bytes.HasPrefix(data, []byte("GIF87a")) || bytes.HasPrefix(data, []byte("GIF89a")))
}

// SniffImageFormat names the container of image bytes: gif, png, jpeg,
// webp, or "" when unknown.
func SniffImageFormat(data []byte) string {
	switch {
	case IsGIFBytes(data):
		return "gif"
	case len(data) >= 8 && bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return "png"
	case len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return "jpeg"
	case len(data) >= 12 && bytes.Equal(data[0:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")):
		return "webp"
	}
	return ""
}

// GIFFrameCountOf counts image descriptors (frames) in a GIF stream by
// walking its blocks. Returns 0 for non-GIF or malformed data.
func GIFFrameCountOf(data []byte) int {
	if !IsGIFBytes(data) || len(data) < 13 {
		return 0
	}
	pos := 13
	if data[10]&0x80 != 0 { // global color table
		pos += 3 * (1 << (int(data[10]&0x07) + 1))
	}
	skipSubBlocks := func() bool {
		for pos < len(data) {
			n := int(data[pos])
			pos++
			if n == 0 {
				return true
			}
			pos += n
		}
		return false
	}
	frames := 0
	for pos < len(data) {
		switch data[pos] {
		case 0x21: // extension
			pos += 2
			if !skipSubBlocks() {
				return frames
			}
		case 0x2C: // image descriptor
			if pos+10 > len(data) {
				return frames
			}
			flags := data[pos+9]
			pos += 10
			if flags&0x80 != 0 { // local color table
				pos += 3 * (1 << (int(flags&0x07) + 1))
			}
			pos++ // LZW minimum code size
			if !skipSubBlocks() {
				return frames + 1
			}
			frames++
		case 0x3B:
			return frames
		default:
			return frames
		}
	}
	return frames
}

func clampInt(v, lo, hi, def int) int {
	if v <= 0 {
		return def
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// NormalizeGIFFrameOptions clamps frame_count / frame_duration_ms, applying
// defaults for missing values.
func NormalizeGIFFrameOptions(frameCount, frameDurationMS int) (int, int) {
	return clampInt(frameCount, MinGIFFrameCount, MaxGIFFrameCount, DefaultGIFFrameCount),
		clampInt(frameDurationMS, MinGIFFrameDurationMS, MaxGIFFrameDurationMS, DefaultGIFFrameDurationMS)
}

// GIFFrameOptionsFromBody reads frame_count / frame_duration_ms for a GIF
// request; non-GIF requests yield zeros.
func GIFFrameOptionsFromBody(body map[string]any) (int, int) {
	if !IsGIFOutputFormat(util.Clean(body["output_format"])) {
		return 0, 0
	}
	return NormalizeGIFFrameOptions(util.ToInt(body["frame_count"], 0), util.ToInt(body["frame_duration_ms"], 0))
}

// GIFPromptInstruction is appended to the prompt sent upstream.
func GIFPromptInstruction(frameCount, frameDurationMS int) string {
	frameCount, frameDurationMS = NormalizeGIFFrameOptions(frameCount, frameDurationMS)
	return fmt.Sprintf(gifPromptInstructionFormat, frameCount, frameDurationMS)
}

// AppendGIFPromptInstruction returns prompt with the GIF instruction appended.
func AppendGIFPromptInstruction(prompt string, frameCount, frameDurationMS int) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return prompt
	}
	return prompt + "\n\n" + GIFPromptInstruction(frameCount, frameDurationMS)
}

// ValidateGIFStreamRequest rejects GIF output combined with stream:true —
// partial frames of an animation cannot be streamed meaningfully.
func ValidateGIFStreamRequest(body map[string]any) error {
	if IsGIFOutputFormat(util.Clean(body["output_format"])) && util.ToBool(body["stream"]) {
		return fmt.Errorf("output_format=gif 不支持 stream=true")
	}
	return nil
}
