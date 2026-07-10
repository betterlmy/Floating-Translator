//go:build windows

package main

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
	bounds := windowBounds{X: 256, Y: 632, Width: 1195, Height: 259}
	got := scaleSubtitleBounds(bounds, 144)
	want := windowBounds{X: 384, Y: 948, Width: 1793, Height: 389}
	if got != want {
		t.Fatalf("scaleSubtitleBounds() = %#v, want %#v", got, want)
	}
}

func TestAnimationAlphaExcludesHoveredDuration(t *testing.T) {
	state := nativeSubtitleState{
		config:    config.SubtitleConfig{DisplayMS: 1000},
		startedAt: time.Now().Add(-3 * time.Second),
		pausedFor: 2500 * time.Millisecond,
	}
	alpha, visible := state.animationAlpha(time.Now())
	if !visible || alpha != 1 {
		t.Fatalf("animationAlpha() = (%v, %t), want (1, true)", alpha, visible)
	}
}
