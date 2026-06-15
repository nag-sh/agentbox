package builder

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

// LayerUtils provides utilities for constructing OCI image layers.
type LayerUtils struct{}

// CreateLayerFromFiles creates an OCI layer from an in-memory map of file paths to contents.
func CreateLayerFromFiles(files map[string][]byte) (v1.Layer, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	now := time.Now()

	seenDirs := make(map[string]bool)

	for path, content := range files {
		// Create directories leading up to the file
		dir := filepath.Dir(path)
		if dir != "." && dir != "/" {
			if err := createTarDirs(tw, dir, now, seenDirs); err != nil {
				return nil, err
			}
		}

		hdr := &tar.Header{
			Name:     stringsTrimPrefix(path, "/"),
			Mode:     0644,
			Size:     int64(len(content)),
			ModTime:  now,
			Typeflag: tar.TypeReg,
		}

		// Ensure scripts are executable
		if stringsHasSuffix(path, ".sh") || filepath.Dir(path) == "bin" || filepath.Dir(path) == "/usr/local/bin" || filepath.Dir(path) == "/opt/agentbox/bin" {
			hdr.Mode = 0755
		}

		if err := tw.WriteHeader(hdr); err != nil {
			return nil, fmt.Errorf("writing header for %s: %w", path, err)
		}

		if _, err := tw.Write(content); err != nil {
			return nil, fmt.Errorf("writing content for %s: %w", path, err)
		}
	}

	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("closing tar writer: %w", err)
	}

	return tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(buf.Bytes())), nil
	})
}

// CreateLayerFromDir creates an OCI layer by packing a local directory.
// The contents of localDir will be placed at targetPath in the layer.
func CreateLayerFromDir(localDir string, targetPath string) (v1.Layer, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	// Trim leading slash for tar header
	targetPath = stringsTrimPrefix(targetPath, "/")
	if targetPath != "" && !stringsHasSuffix(targetPath, "/") {
		targetPath += "/"
	}

	err := filepath.Walk(localDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(localDir, path)
		if err != nil {
			return err
		}

		if relPath == "." {
			return nil
		}

		targetName := targetPath + relPath

		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		
		hdr.Name = targetName
		
		// Normalize ownership and times for reproducible builds
		hdr.Uid = 0
		hdr.Gid = 0
		hdr.Uname = "root"
		hdr.Gname = "root"
		hdr.ModTime = time.Unix(0, 0)

		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}

		if !info.IsDir() && info.Mode().IsRegular() {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()

			if _, err := io.Copy(tw, f); err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("walking directory %s: %w", localDir, err)
	}

	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("closing tar writer: %w", err)
	}

	return tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(buf.Bytes())), nil
	})
}

// AppendLayers appends the given layers to a base image.
func AppendLayers(base v1.Image, layers ...v1.Layer) (v1.Image, error) {
	if base == nil {
		base = empty.Image
	}
	
	// Add layers one by one
	img := base
	var err error
	for _, layer := range layers {
		img, err = mutate.AppendLayers(img, layer)
		if err != nil {
			return nil, fmt.Errorf("appending layer: %w", err)
		}
	}
	
	return img, nil
}

// createTarDirs creates directory entries in a tar writer for all parent directories.
func createTarDirs(tw *tar.Writer, dir string, t time.Time, seen map[string]bool) error {
	dir = stringsTrimPrefix(dir, "/")
	if dir == "" || dir == "." {
		return nil
	}

	parts := stringsSplit(dir, "/")
	current := ""
	for _, part := range parts {
		if current == "" {
			current = part
		} else {
			current = current + "/" + part
		}

		dirName := current + "/"
		if seen[dirName] {
			continue
		}
		seen[dirName] = true

		hdr := &tar.Header{
			Name:     dirName,
			Mode:     0755,
			Typeflag: tar.TypeDir,
			ModTime:  t,
		}

		_ = tw.WriteHeader(hdr)
	}
	
	return nil
}

// String utilities without depending on strings package

func stringsTrimPrefix(s, prefix string) string {
	if len(s) >= len(prefix) && s[0:len(prefix)] == prefix {
		return s[len(prefix):]
	}
	return s
}

func stringsHasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

func stringsSplit(s, sep string) []string {
	if s == "" {
		return nil
	}
	var res []string
	start := 0
	for i := 0; i < len(s)-len(sep)+1; i++ {
		if s[i:i+len(sep)] == sep {
			res = append(res, s[start:i])
			start = i + len(sep)
			i += len(sep) - 1
		}
	}
	res = append(res, s[start:])
	return res
}

// CleanLayers converts a slice of custom layers into standard OCI layers.
func CleanLayers(layers []v1.Layer) ([]v1.Layer, error) {
	var clean []v1.Layer
	for _, l := range layers {
		cl, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
			return l.Uncompressed()
		})
		if err != nil {
			return nil, fmt.Errorf("cleaning layer: %w", err)
		}
		clean = append(clean, cl)
	}
	return clean, nil
}
