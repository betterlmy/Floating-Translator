//go:build windows

package main

import (
	"sort"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const windowsFontsRegistryPath = `SOFTWARE\Microsoft\Windows NT\CurrentVersion\Fonts`

// GetAvailableFonts returns installed Windows font family names for the
// settings search control. Registry value names are the family names shown to
// users, while their values only contain font-file paths and are not exposed.
func (a *App) GetAvailableFonts() ([]string, error) {
	fonts := map[string]struct{}{"Microsoft YaHei UI": {}}
	var firstError error
	for _, root := range []registry.Key{registry.LOCAL_MACHINE, registry.CURRENT_USER} {
		key, err := registry.OpenKey(root, windowsFontsRegistryPath, registry.QUERY_VALUE)
		if err != nil {
			if firstError == nil {
				firstError = err
			}
			continue
		}
		names, err := key.ReadValueNames(0)
		_ = key.Close()
		if err != nil {
			if firstError == nil {
				firstError = err
			}
			continue
		}
		for _, name := range names {
			if family := fontFamilyFromRegistryName(name); family != "" {
				fonts[family] = struct{}{}
			}
		}
	}
	result := make([]string, 0, len(fonts))
	for font := range fonts {
		result = append(result, font)
	}
	sort.Strings(result)
	if len(result) == 1 && firstError != nil {
		return result, firstError
	}
	return result, nil
}

func fontFamilyFromRegistryName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.TrimSuffix(name, " (TrueType)")
	name = strings.TrimSuffix(name, " (OpenType)")
	if name == "" || strings.HasPrefix(name, "@") {
		return ""
	}
	return name
}
