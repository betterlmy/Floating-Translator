package logger

import (
	"path/filepath"
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
