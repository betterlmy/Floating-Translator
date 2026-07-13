//go:build darwin

package fonts

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

// List 返回 macOS 已安装的字体族名称。
// settings search control. Font file paths and other metadata are not exposed.
func List() ([]string, error) {
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
