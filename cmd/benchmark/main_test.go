package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kungfudaibi/llm-serving-guardian/internal/benchmark"
)

func TestWriteReportCreatesFileAndRefusesOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	want := []byte("{\"schemaVersion\":1}\n")

	if err := benchmark.WriteReport(path, want); err != nil {
		t.Fatalf("WriteReport() error = %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("report = %q, want %q", got, want)
	}
	if err := benchmark.WriteReport(path, []byte("overwrite")); err == nil {
		t.Fatal("WriteReport() unexpectedly overwrote an existing report")
	}
}
