package hotkey

import "testing"

func TestParseDefaultShortcut(t *testing.T) {
	shortcut, err := Parse("Ctrl+Alt+T")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if shortcut.Modifiers != ModifierControl|ModifierAlt || shortcut.VirtualKey != 'T' {
		t.Fatalf("shortcut = %+v", shortcut)
	}
	if shortcut.Canonical != "Ctrl+Alt+T" {
		t.Fatalf("Canonical = %q", shortcut.Canonical)
	}
}

func TestParseNormalizesShortcut(t *testing.T) {
	shortcut, err := Parse(" shift + control + f12 ")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if shortcut.Canonical != "Ctrl+Shift+F12" {
		t.Fatalf("Canonical = %q", shortcut.Canonical)
	}
}

func TestParseRejectsInvalidShortcut(t *testing.T) {
	testCases := []string{"T", "Ctrl+", "Ctrl+Alt+T+Y", "Ctrl+Ctrl+T", "Ctrl+Space"}
	for _, testCase := range testCases {
		t.Run(testCase, func(t *testing.T) {
			if _, err := Parse(testCase); err == nil {
				t.Fatal("Parse() error = nil")
			}
		})
	}
}
