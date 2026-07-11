//go:build darwin

package main

/*
#cgo darwin LDFLAGS: -framework Cocoa
#include <stdlib.h>

char *macosAvailableFontFamilies(void);
void macosFreeString(char *value);
*/
import "C"

import (
	"fmt"
	"sort"
	"strings"
)

// GetAvailableFonts returns installed macOS font family names for the
// settings search control. Font file paths and other metadata are not exposed.
func (a *App) GetAvailableFonts() ([]string, error) {
	fonts := map[string]struct{}{
		"Helvetica Neue": {},
		"PingFang SC":    {},
		"SF Pro":         {},
	}
	value := C.macosAvailableFontFamilies()
	if value == nil {
		return sortedFonts(fonts), fmt.Errorf("读取 macOS 字体列表失败")
	}
	defer C.macosFreeString(value)
	for _, family := range strings.Split(C.GoString(value), "\n") {
		family = strings.TrimSpace(family)
		if family != "" {
			fonts[family] = struct{}{}
		}
	}
	return sortedFonts(fonts), nil
}

func sortedFonts(fonts map[string]struct{}) []string {
	result := make([]string, 0, len(fonts))
	for font := range fonts {
		result = append(result, font)
	}
	sort.Strings(result)
	return result
}
