package builder

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestCreateLayerFromFiles(t *testing.T) {
	files := map[string][]byte{
		"/opt/agentbox/config/runtime.yaml": []byte("harness:\n  command: opencode\n"),
		"/opt/agentbox/bin/agentbox-init":   []byte("#!/bin/sh\n"),
	}

	layer, err := CreateLayerFromFiles(files)
	if err != nil {
		t.Fatalf("CreateLayerFromFiles failed: %v", err)
	}

	rc, err := layer.Uncompressed()
	if err != nil {
		t.Fatalf("Uncompressed failed: %v", err)
	}
	defer rc.Close()

	tr := tar.NewReader(rc)
	found := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar read: %v", err)
		}
		if hdr.Name == "opt/agentbox/config/runtime.yaml" || hdr.Name == "opt/agentbox/bin/agentbox-init" {
			found++
			if hdr.Name == "opt/agentbox/bin/agentbox-init" && hdr.Mode != 0755 {
				t.Errorf("expected executable mode for init binary, got %o", hdr.Mode)
			}
		}
	}
	if found != 2 {
		t.Errorf("expected 2 files, found %d", found)
	}
}

func TestCreateLayerFromDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	layer, err := CreateLayerFromDir(dir, "/opt/data")
	if err != nil {
		t.Fatalf("CreateLayerFromDir failed: %v", err)
	}

	rc, err := layer.Uncompressed()
	if err != nil {
		t.Fatalf("Uncompressed failed: %v", err)
	}
	defer rc.Close()

	tr := tar.NewReader(rc)
	found := false
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar read: %v", err)
		}
		if hdr.Name == "opt/data/hello.txt" {
			found = true
			var buf bytes.Buffer
			if _, err := io.Copy(&buf, tr); err != nil {
				t.Fatalf("read file: %v", err)
			}
			if buf.String() != "hello" {
				t.Errorf("unexpected content: %q", buf.String())
			}
		}
	}
	if !found {
		t.Error("expected hello.txt in layer")
	}
}
