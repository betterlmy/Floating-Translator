//go:build darwin

package platform

import (
	"context"
	"errors"
	"testing"

	"floating-translator/internal/hotkey"
)

func TestContextError(t *testing.T) {
	if err := contextError(context.Background()); err != nil {
		t.Fatalf("contextError(background) = %v, want nil", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := contextError(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("contextError(cancelled) = %v, want context.Canceled", err)
	}
}

func TestShortcutsEqual(t *testing.T) {
	shortcut := &hotkey.Shortcut{Modifiers: hotkey.ModifierWin, VirtualKey: 'T'}
	sameShortcut := &hotkey.Shortcut{Modifiers: hotkey.ModifierWin, VirtualKey: 'T'}
	differentShortcut := &hotkey.Shortcut{Modifiers: hotkey.ModifierAlt, VirtualKey: 'T'}

	if !shortcutsEqual(nil, nil) || shortcutsEqual(shortcut, nil) {
		t.Fatal("nil 快捷键比较结果错误")
	}
	if !shortcutsEqual(shortcut, sameShortcut) {
		t.Fatal("相同快捷键应视为相等")
	}
	if shortcutsEqual(shortcut, differentShortcut) {
		t.Fatal("不同修饰键不应视为相等")
	}
}

func TestDarwinOpenPathRejectsEmptyPath(t *testing.T) {
	desktop := NewDesktop()
	if err := desktop.OpenPath(" "); err == nil {
		t.Fatal("空路径应被拒绝")
	}
}
