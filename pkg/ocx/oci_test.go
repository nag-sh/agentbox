package ocx

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractTarGz(t *testing.T) {
	dest := t.TempDir()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tr := tar.NewWriter(gz)

	files := map[string][]byte{
		"skills/researcher.md": []byte("# Researcher"),
		"agents/coder.md":      []byte("# Coder"),
	}

	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0644,
			Size: int64(len(content)),
		}
		if err := tr.WriteHeader(hdr); err != nil {
			t.Fatalf("write header: %v", err)
		}
		if _, err := tr.Write(content); err != nil {
			t.Fatalf("write content: %v", err)
		}
	}

	if err := tr.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}

	if err := extractTarGz(bytes.NewReader(buf.Bytes()), dest); err != nil {
		t.Fatalf("extract: %v", err)
	}

	for name, want := range files {
		path := filepath.Join(dest, name)
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s: got %q, want %q", name, got, want)
		}
	}
}

func TestExtractTarGz_RejectsPathEscape(t *testing.T) {
	dest := t.TempDir()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tr := tar.NewWriter(gz)

	hdr := &tar.Header{
		Name: "../../etc/passwd",
		Mode: 0644,
		Size: 4,
	}
	tr.WriteHeader(hdr)
	tr.Write([]byte("root"))
	tr.Close()
	gz.Close()

	if err := extractTarGz(bytes.NewReader(buf.Bytes()), dest); err == nil {
		t.Error("expected error for escaping tar entry")
	}
}

func TestIsWithinDest(t *testing.T) {
	dest := "/tmp/ocx"
	cases := []struct {
		target string
		want   bool
	}{
		{"/tmp/ocx/file", true},
		{"/tmp/ocx", true},
		{"/tmp/ocx/sub/file", true},
		{"/tmp/other", false},
		{"/tmp/ocx../file", false},
	}
	for _, tc := range cases {
		if got := isWithinDest(tc.target, dest); got != tc.want {
			t.Errorf("%q: got %v, want %v", tc.target, got, tc.want)
		}
	}
}

func TestOCIFetcher_Fetch_InvalidReference(t *testing.T) {
	fetcher := NewOCIFetcher(nil)
	_, _, err := fetcher.Fetch(t.Context(), "not a valid reference")
	if err == nil {
		t.Fatal("expected error for invalid reference")
	}
}

func TestOCIFetcher_Fetch_NilClient(t *testing.T) {
	fetcher := NewOCIFetcher(nil)
	_, _, err := fetcher.Fetch(t.Context(), "example.com/foo:bar")
	if err == nil {
		t.Fatal("expected error with nil client")
	}
}
