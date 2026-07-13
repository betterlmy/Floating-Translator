//go:build windows

package subtitle

import (
	"testing"
	"time"

	"floating-translator/internal/config"
)

func TestSubtitleStagePaddingMatchesCSSClamp(t *testing.T) {
	tests := []struct {
		height int
		want   int
	}{
		{height: 300, want: 14},
		{height: 1000, want: 22},
		{height: 2000, want: 28},
	}
	for _, test := range tests {
		if got := subtitleStagePadding(test.height, 96); got != test.want {
			t.Errorf("subtitleStagePadding(%d) = %d, want %d", test.height, got, test.want)
		}
	}
}

func TestScaleSubtitleBoundsForPhysicalPixels(t *testing.T) {
	bounds := Bounds{X: 256, Y: 632, Width: 1195, Height: 259}
	got := scaleSubtitleBounds(bounds, 144)
	want := Bounds{X: 384, Y: 948, Width: 1793, Height: 389}
	if got != want {
		t.Fatalf("scaleSubtitleBounds() = %#v, want %#v", got, want)
	}
}

func TestRestartDisplayAfterHoverRestartsFullDisplayDuration(t *testing.T) {
	now := time.Now()
	state := nativeSubtitleState{
		config: config.SubtitleConfig{FadeInMS: 200, DisplayMS: 1000, FadeOutMS: 800},
	}
	state.restartDisplayAfterHover(now)

	alpha, visible := state.animationAlpha(now.Add(999 * time.Millisecond))
	if !visible || alpha != 1 {
		t.Fatalf("animationAlpha() = (%v, %t), want (1, true)", alpha, visible)
	}
	if state.lastAlpha != -1 {
		t.Fatalf("lastAlpha = %v, want -1", state.lastAlpha)
	}
}

func TestSubtitleScrollOffsetFollowsPauseTravelAndBottomPause(t *testing.T) {
	now := time.Now()
	state := nativeSubtitleState{startedAt: now}
	if got := state.subtitleScrollOffset(now.Add(500*time.Millisecond), 240, 96); got != 0 {
		t.Fatalf("顶部停留偏移 = %v, want 0", got)
	}
	if got := state.subtitleScrollOffset(now.Add(6*time.Second), 240, 96); got != 120 {
		t.Fatalf("滚动中偏移 = %v, want 120", got)
	}
	if got := state.subtitleScrollOffset(now.Add(12*time.Second), 240, 96); got != 240 {
		t.Fatalf("底部停留偏移 = %v, want 240", got)
	}
	state.persistent = true
	if got := state.subtitleScrollOffset(now.Add(6*time.Second), 240, 96); got != 0 {
		t.Fatalf("持久字幕偏移 = %v, want 0", got)
	}
}

func TestSubtitleHexColor(t *testing.T) {
	red, green, blue := subtitleHexColor("#12AbF0")
	if red != 0x12 || green != 0xAB || blue != 0xF0 {
		t.Fatalf("subtitleHexColor() = (%#x, %#x, %#x)", red, green, blue)
	}
}

func TestSubtitleRenderBackoffIsBounded(t *testing.T) {
	if got := subtitleRenderBackoff(1); got != nativeSubtitleInitialBackoff {
		t.Fatalf("subtitleRenderBackoff(1) = %s, want %s", got, nativeSubtitleInitialBackoff)
	}
	if got := subtitleRenderBackoff(nativeSubtitleMaxFailures + 10); got != nativeSubtitleMaxBackoff {
		t.Fatalf("subtitleRenderBackoff() = %s, want %s", got, nativeSubtitleMaxBackoff)
	}
}
