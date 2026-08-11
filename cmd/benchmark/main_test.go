package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteReportCreatesFileAndRefusesOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	want := []byte("{\"schemaVersion\":1}\n")

	if err := writeReport(path, want); err != nil {
		t.Fatalf("writeReport() error = %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("report = %q, want %q", got, want)
	}
	if err := writeReport(path, []byte("overwrite")); err == nil {
		t.Fatal("writeReport() unexpectedly overwrote an existing report")
	}
}
