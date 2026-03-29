package engine

import (
	"encoding/json"
	"testing"
)

func TestPixelSurface(t *testing.T) {
	cfg := PixelSurface("game", 320, 240)
	if cfg.Name != "game" {
		t.Fatalf("expected name 'game', got %q", cfg.Name)
	}
	if cfg.Kind != KindSurface {
		t.Fatalf("expected surface kind")
	}
	if cfg.MountID != "gosx-pixel-game" {
		t.Fatalf("expected mount id 'gosx-pixel-game', got %q", cfg.MountID)
	}
	if cfg.PixelSurface == nil {
		t.Fatal("expected pixel surface config")
	}
	if cfg.PixelSurface.Width != 320 || cfg.PixelSurface.Height != 240 {
		t.Fatalf("expected 320x240, got %dx%d", cfg.PixelSurface.Width, cfg.PixelSurface.Height)
	}
	if cfg.PixelSurface.Scaling != ScalePixelPerfect {
		t.Fatalf("expected pixel-perfect scaling, got %q", cfg.PixelSurface.Scaling)
	}
	if !cfg.PixelSurface.VSyncEnabled() {
		t.Fatal("expected vsync enabled by default")
	}
}

func TestPixelSurfaceOptions(t *testing.T) {
	cfg := PixelSurface("retro", 160, 144,
		WithScaling(ScaleFill),
		WithClearColor(0, 0, 0, 255),
		WithVSync(false),
	)
	if cfg.PixelSurface.Scaling != ScaleFill {
		t.Fatalf("expected fill scaling, got %q", cfg.PixelSurface.Scaling)
	}
	if cfg.PixelSurface.ClearColor != [4]uint8{0, 0, 0, 255} {
		t.Fatalf("unexpected clear color: %v", cfg.PixelSurface.ClearColor)
	}
	if cfg.PixelSurface.VSyncEnabled() {
		t.Fatal("expected vsync disabled")
	}
}

func TestPixelSurfaceJSON(t *testing.T) {
	cfg := PixelSurface("screen", 256, 256, WithScaling(ScaleFill))
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}

	var decoded Config
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.PixelSurface == nil {
		t.Fatal("expected pixel surface in decoded config")
	}
	if decoded.PixelSurface.Width != 256 {
		t.Fatalf("expected width 256, got %d", decoded.PixelSurface.Width)
	}
	if decoded.PixelSurface.Scaling != ScaleFill {
		t.Fatalf("expected fill scaling, got %q", decoded.PixelSurface.Scaling)
	}
}

func TestScalingTransformPixelPerfect(t *testing.T) {
	// 320x240 on a 1920x1080 surface: max integer scale is 4 (1280x960)
	scale, ox, oy := ScalingTransform(320, 240, 1920, 1080, ScalePixelPerfect)
	if scale != 4 {
		t.Fatalf("expected scale 4, got %f", scale)
	}
	// Centered: (1920 - 1280) / 2 = 320
	if ox != 320 {
		t.Fatalf("expected offsetX 320, got %f", ox)
	}
	// Centered: (1080 - 960) / 2 = 60
	if oy != 60 {
		t.Fatalf("expected offsetY 60, got %f", oy)
	}
}

func TestScalingTransformFill(t *testing.T) {
	// 320x240 on 1920x1080: scaleX=6, scaleY=4.5, min=4.5
	scale, ox, oy := ScalingTransform(320, 240, 1920, 1080, ScaleFill)
	if scale != 4.5 {
		t.Fatalf("expected scale 4.5, got %f", scale)
	}
	// Centered: (1920 - 1440) / 2 = 240
	if ox != 240 {
		t.Fatalf("expected offsetX 240, got %f", ox)
	}
	if oy != 0 {
		t.Fatalf("expected offsetY 0, got %f", oy)
	}
}

func TestWindowToPixel(t *testing.T) {
	// 320x240 pixel-perfect on 1920x1080: scale=4, offset=(320, 60)
	px, py, inside := WindowToPixel(480, 180, 320, 240, 1920, 1080, ScalePixelPerfect)
	// (480 - 320) / 4 = 40, (180 - 60) / 4 = 30
	if px != 40 || py != 30 {
		t.Fatalf("expected (40, 30), got (%d, %d)", px, py)
	}
	if !inside {
		t.Fatal("expected inside")
	}
}

func TestWindowToPixelOutside(t *testing.T) {
	// Click in the letterbox area
	_, _, inside := WindowToPixel(10, 10, 320, 240, 1920, 1080, ScalePixelPerfect)
	if inside {
		t.Fatal("expected outside — click is in letterbox area")
	}
}

func TestScalingTransformZero(t *testing.T) {
	scale, ox, oy := ScalingTransform(0, 0, 1920, 1080, ScalePixelPerfect)
	if scale != 1 || ox != 0 || oy != 0 {
		t.Fatalf("expected identity for zero buffer, got scale=%f ox=%f oy=%f", scale, ox, oy)
	}
}
