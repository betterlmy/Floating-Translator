//go:build !windows && !darwin

package fonts

// List 在不支持的平台返回默认字体族。
func List() ([]string, error) {
	return []string{"Microsoft YaHei UI"}, nil
}
