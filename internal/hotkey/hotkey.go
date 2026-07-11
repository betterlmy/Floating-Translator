// Package hotkey 负责解析全局快捷键配置。
package hotkey

import (
	"fmt"
	"strconv"
	"strings"
)

// Modifiers 是跨平台快捷键修饰键位掩码。ModifierWin 在 macOS 上映射为 Command。
type Modifiers uint32

const (
	ModifierAlt     Modifiers = 0x0001
	ModifierControl Modifiers = 0x0002
	ModifierShift   Modifiers = 0x0004
	ModifierWin     Modifiers = 0x0008
)

// Shortcut 是已校验并规范化的全局快捷键。
type Shortcut struct {
	Modifiers  Modifiers
	VirtualKey uint32
	Canonical  string
}

// Parse 解析 Ctrl+Alt+T 或 Command+Option+T 一类快捷键，支持字母、数字和 F1-F24。
func Parse(value string) (Shortcut, error) {
	parts := strings.Split(value, "+")
	if len(parts) < 2 {
		return Shortcut{}, fmt.Errorf("快捷键必须包含至少一个修饰键和一个按键")
	}

	var modifiers Modifiers
	var virtualKey uint32
	var keyName string
	for _, rawPart := range parts {
		part := strings.ToUpper(strings.TrimSpace(rawPart))
		if part == "" {
			return Shortcut{}, fmt.Errorf("快捷键包含空按键")
		}
		switch part {
		case "CTRL", "CONTROL":
			if modifiers&ModifierControl != 0 {
				return Shortcut{}, fmt.Errorf("快捷键重复配置 Ctrl")
			}
			modifiers |= ModifierControl
		case "ALT":
			if modifiers&ModifierAlt != 0 {
				return Shortcut{}, fmt.Errorf("快捷键重复配置 Alt")
			}
			modifiers |= ModifierAlt
		case "OPTION":
			if modifiers&ModifierAlt != 0 {
				return Shortcut{}, fmt.Errorf("快捷键重复配置 Alt/Option")
			}
			modifiers |= ModifierAlt
		case "SHIFT":
			if modifiers&ModifierShift != 0 {
				return Shortcut{}, fmt.Errorf("快捷键重复配置 Shift")
			}
			modifiers |= ModifierShift
		case "WIN", "SUPER", "CMD", "COMMAND", "META":
			if modifiers&ModifierWin != 0 {
				return Shortcut{}, fmt.Errorf("快捷键重复配置 Win/Command")
			}
			modifiers |= ModifierWin
		default:
			if virtualKey != 0 {
				return Shortcut{}, fmt.Errorf("快捷键只能包含一个普通按键")
			}
			var err error
			virtualKey, keyName, err = parseKey(part)
			if err != nil {
				return Shortcut{}, err
			}
		}
	}
	if modifiers == 0 || virtualKey == 0 {
		return Shortcut{}, fmt.Errorf("快捷键必须包含至少一个修饰键和一个按键")
	}

	canonicalParts := make([]string, 0, 5)
	if modifiers&ModifierControl != 0 {
		canonicalParts = append(canonicalParts, "Ctrl")
	}
	if modifiers&ModifierAlt != 0 {
		canonicalParts = append(canonicalParts, "Alt")
	}
	if modifiers&ModifierShift != 0 {
		canonicalParts = append(canonicalParts, "Shift")
	}
	if modifiers&ModifierWin != 0 {
		canonicalParts = append(canonicalParts, "Win")
	}
	canonicalParts = append(canonicalParts, keyName)
	return Shortcut{
		Modifiers:  modifiers,
		VirtualKey: virtualKey,
		Canonical:  strings.Join(canonicalParts, "+"),
	}, nil
}

// MacCanonical returns the native macOS symbol form, for example ⌘⌥T.
func (shortcut Shortcut) MacCanonical() string {
	parts := make([]string, 0, 5)
	if shortcut.Modifiers&ModifierWin != 0 {
		parts = append(parts, "⌘")
	}
	if shortcut.Modifiers&ModifierControl != 0 {
		parts = append(parts, "⌃")
	}
	if shortcut.Modifiers&ModifierAlt != 0 {
		parts = append(parts, "⌥")
	}
	if shortcut.Modifiers&ModifierShift != 0 {
		parts = append(parts, "⇧")
	}
	if shortcut.Canonical != "" {
		canonicalParts := strings.Split(shortcut.Canonical, "+")
		parts = append(parts, canonicalParts[len(canonicalParts)-1])
	}
	return strings.Join(parts, "")
}

func parseKey(value string) (uint32, string, error) {
	if len(value) == 1 {
		key := value[0]
		if key >= 'A' && key <= 'Z' || key >= '0' && key <= '9' {
			return uint32(key), value, nil
		}
	}
	if strings.HasPrefix(value, "F") {
		number, err := strconv.Atoi(strings.TrimPrefix(value, "F"))
		if err == nil && number >= 1 && number <= 24 {
			return uint32(0x70 + number - 1), fmt.Sprintf("F%d", number), nil
		}
	}
	return 0, "", fmt.Errorf("不支持的快捷键按键 %q，仅支持字母、数字和 F1-F24", value)
}
