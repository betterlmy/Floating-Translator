package logger

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestReconfigureUpdatesRotationSettings(t *testing.T) {
	log, err := New(filepath.Join(t.TempDir(), "app.log"), "info", 10, 3)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := log.Reconfigure(64, 7); err != nil {
		t.Fatalf("Reconfigure() error = %v", err)
	}
	if got := log.writer.logger.MaxSize; got != 64 {
		t.Fatalf("MaxSize = %d, want 64", got)
	}
	if got := log.writer.logger.MaxBackups; got != 7 {
		t.Fatalf("MaxBackups = %d, want 7", got)
	}
}

func TestReconfigureRejectsInvalidValues(t *testing.T) {
	log := NewNop()
	if err := log.Reconfigure(0, 1); err == nil {
		t.Fatal("Reconfigure() should reject a zero max size")
	}
	if err := log.Reconfigure(1, -1); err == nil {
		t.Fatal("Reconfigure() should reject negative backups")
	}
}

func TestSanitizeErrorRedactsCredentialsURLsAndLength(t *testing.T) {
	secret := strings.Repeat("x", 700)
	err := errors.New("request failed https://api.example.com/v1?token=" + secret + " Authorization: Bearer abcdefghijklmnopqrstuvwxyz")
	message := SanitizeError(err)
	if strings.Contains(message, secret) || strings.Contains(message, "abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("SanitizeError() 泄露了敏感内容: %q", message)
	}
	if len([]rune(message)) > maxSanitizedErrorRunes+1 {
		t.Fatalf("SanitizeError() 结果过长: %d", len([]rune(message)))
	}
}
