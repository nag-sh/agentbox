package ocx

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"

	"github.com/nag-sh/agentbox/pkg/registry"
)

// OCX OCI artifact media types. An OCX component is packaged as an OCI
// artifact whose config descriptor uses MediaTypeComponentConfig and whose
// file layers use MediaTypeComponentFiles.
const (
	MediaTypeComponentConfig = "application/vnd.ocx.component.config.v1+json"
	MediaTypeComponentFiles  = "application/vnd.ocx.component.files.v1+tar.gzip"
)

// OCIFetcher pulls OCX component artifacts from an OCI registry.
type OCIFetcher struct {
	client *registry.Client
}

// NewOCIFetcher creates a fetcher backed by the supplied registry client.
func NewOCIFetcher(client *registry.Client) *OCIFetcher {
	return &OCIFetcher{client: client}
}

// Fetch pulls the OCX component at ref, returning its parsed manifest and a
// temporary directory containing the component files. The caller is
// responsible for removing the directory when done.
func (f *OCIFetcher) Fetch(ctx context.Context, ref string) (*ComponentManifest, string, error) {
	if f.client == nil {
		return nil, "", fmt.Errorf("registry client is required")
	}

	parsedRef, err := name.ParseReference(ref)
	if err != nil {
		return nil, "", fmt.Errorf("parsing reference %q: %w", ref, err)
	}

	repo, err := f.repository(ctx, parsedRef)
	if err != nil {
		return nil, "", err
	}

	desc, manifestReader, err := repo.FetchReference(ctx, parsedRef.Identifier())
	if err != nil {
		return nil, "", fmt.Errorf("fetching manifest for %q: %w", ref, err)
	}
	defer manifestReader.Close()

	if desc.MediaType != ocispec.MediaTypeImageManifest {
		return nil, "", fmt.Errorf("unsupported manifest media type %q", desc.MediaType)
	}

	manifestBytes, err := io.ReadAll(manifestReader)
	if err != nil {
		return nil, "", fmt.Errorf("reading manifest: %w", err)
	}

	var manifest ocispec.Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return nil, "", fmt.Errorf("parsing manifest: %w", err)
	}

	if manifest.Config.MediaType != MediaTypeComponentConfig {
		return nil, "", fmt.Errorf("unexpected config media type %q, want %q", manifest.Config.MediaType, MediaTypeComponentConfig)
	}

	component, err := f.fetchConfig(ctx, repo, manifest.Config)
	if err != nil {
		return nil, "", fmt.Errorf("fetching component config: %w", err)
	}

	staging, err := os.MkdirTemp("", "agentbox-ocx-")
	if err != nil {
		return nil, "", fmt.Errorf("creating staging dir: %w", err)
	}

	if err := f.extractLayers(ctx, repo, manifest.Layers, staging); err != nil {
		os.RemoveAll(staging)
		return nil, "", fmt.Errorf("extracting component files: %w", err)
	}

	return component, staging, nil
}

func (f *OCIFetcher) repository(ctx context.Context, ref name.Reference) (*remote.Repository, error) {
	repo, err := remote.NewRepository(ref.Context().Name())
	if err != nil {
		return nil, fmt.Errorf("creating repository: %w", err)
	}

	keychain := f.client.AuthenticatorWrapper()
	repo.Client = &auth.Client{
		Client: retry.DefaultClient,
		Cache:  auth.NewCache(),
		Credential: func(ctx context.Context, host string) (auth.Credential, error) {
			target, err := name.NewRegistry(host)
			if err != nil {
				return auth.EmptyCredential, err
			}
			authenticator, err := keychain.Resolve(target)
			if err != nil {
				return auth.EmptyCredential, err
			}
			if authenticator == authn.Anonymous {
				return auth.EmptyCredential, nil
			}
			authConfig, err := authenticator.Authorization()
			if err != nil {
				return auth.EmptyCredential, err
			}
			return auth.Credential{
				Username:     authConfig.Username,
				Password:     authConfig.Password,
				RefreshToken: authConfig.RegistryToken,
			}, nil
		},
	}
	return repo, nil
}

func (f *OCIFetcher) fetchConfig(ctx context.Context, repo *remote.Repository, desc ocispec.Descriptor) (*ComponentManifest, error) {
	rc, err := repo.Blobs().Fetch(ctx, desc)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}

	var component ComponentManifest
	if err := json.Unmarshal(data, &component); err != nil {
		return nil, fmt.Errorf("parsing component config: %w", err)
	}
	if err := component.Validate(); err != nil {
		return nil, fmt.Errorf("invalid component config: %w", err)
	}
	return &component, nil
}

func (f *OCIFetcher) extractLayers(ctx context.Context, repo *remote.Repository, layers []ocispec.Descriptor, dest string) error {
	for _, layer := range layers {
		if layer.MediaType != MediaTypeComponentFiles {
			continue
		}

		rc, err := repo.Blobs().Fetch(ctx, layer)
		if err != nil {
			return fmt.Errorf("fetching layer %s: %w", layer.Digest, err)
		}

		if err := extractTarGz(rc, dest); err != nil {
			rc.Close()
			return fmt.Errorf("extracting layer %s: %w", layer.Digest, err)
		}
		if err := rc.Close(); err != nil {
			return fmt.Errorf("closing layer reader: %w", err)
		}
	}
	return nil
}

func extractTarGz(r io.Reader, dest string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("creating gzip reader: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading tar header: %w", err)
		}

		target := filepath.Join(dest, header.Name)
		if !isWithinDest(target, dest) {
			return fmt.Errorf("tar entry escapes destination: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(file, tr); err != nil {
				file.Close()
				return err
			}
			if err := file.Close(); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			if err := os.Symlink(header.Linkname, target); err != nil {
				return err
			}
		case tar.TypeLink:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			linkTarget := filepath.Join(dest, header.Linkname)
			if !isWithinDest(linkTarget, dest) {
				return fmt.Errorf("hard link target escapes destination: %s", header.Linkname)
			}
			if err := os.Link(linkTarget, target); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported tar entry type %d: %s", header.Typeflag, header.Name)
		}
	}
	return nil
}

func isWithinDest(target, dest string) bool {
	cleanTarget := filepath.Clean(target)
	cleanDest := filepath.Clean(dest)
	if cleanTarget == cleanDest {
		return true
	}
	return strings.HasPrefix(cleanTarget, cleanDest+string(filepath.Separator))
}
