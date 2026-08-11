package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadFaultMarkerParsesRFC3339Nano(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fault-at.txt")
	want := time.Date(2026, 8, 11, 8, 1, 2, 345678901, time.UTC)
	if err := os.WriteFile(path, []byte(want.Format(time.RFC3339Nano)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := readFaultMarker(path)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(want) {
		t.Fatalf("fault time = %s, want %s", got, want)
	}
}
