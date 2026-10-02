package discovery

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestArtefakPhase0Lengkap(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("lokasi test tidak dapat ditentukan")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))

	for _, masalah := range Validate(root) {
		t.Errorf("%s: %s", masalah.Path, masalah.Message)
	}
}
