//go:build windows

package main

import "testing"

func TestFontFamilyFromRegistryName(t *testing.T) {
	tests := map[string]string{
		"Segoe UI (TrueType)": "Segoe UI",
		"Aptos (OpenType)":    "Aptos",
		"@Vertical Font":      "",
	}
	for input, want := range tests {
		if got := fontFamilyFromRegistryName(input); got != want {
			t.Errorf("fontFamilyFromRegistryName(%q) = %q, want %q", input, got, want)
		}
	}
}
