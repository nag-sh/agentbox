package registry

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/file"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
)

// ArtifactType represents the type of an OCI artifact stored by agentbox.
type ArtifactType string

const (
	ArtifactTypeSkill       ArtifactType = "application/vnd.agentbox.skill.v1+tar.gzip"
	ArtifactTypePlugin      ArtifactType = "application/vnd.agentbox.plugin.v1+tar.gzip"
	ArtifactTypeMCP         ArtifactType = "application/vnd.agentbox.mcp-server.v1+tar.gzip"
	ArtifactTypeHarness     ArtifactType = "application/vnd.agentbox.harness.v1+tar.gzip"
	ArtifactTypeOCXConfig   ArtifactType = "application/vnd.ocx.component.config.v1+json"
	ArtifactTypeOCXFiles    ArtifactType = "application/vnd.ocx.component.files.v1+tar.gzip"
)

// ParseArtifactType converts a string ("skill", "plugin", "mcp", "harness") to its full ArtifactType.
func ParseArtifactType(s string) (ArtifactType, error) {
	switch strings.ToLower(s) {
	case "skill":
		return ArtifactTypeSkill, nil
	case "plugin":
		return ArtifactTypePlugin, nil
	case "mcp":
		return ArtifactTypeMCP, nil
	case "harness":
		return ArtifactTypeHarness, nil
	default:
		return "", fmt.Errorf("unknown artifact type %q", s)
	}
}

// ArtifactInfo contains metadata about a pulled artifact.
type ArtifactInfo struct {
	Digest string
	Tag    string
}

// ArtifactStore manages pushing and pulling ORAS artifacts.
type ArtifactStore struct {
	client *Client
}

// NewArtifactStore creates a new ArtifactStore using the provided registry client.
func NewArtifactStore(client *Client) *ArtifactStore {
	return &ArtifactStore{client: client}
}

// getORASRepository returns an ORAS remote.Repository configured with auth.
func (s *ArtifactStore) getORASRepository(ctx context.Context, ref string) (*remote.Repository, error) {
	parsedRef, err := name.ParseReference(ref)
	if err != nil {
		return nil, fmt.Errorf("parsing reference: %w", err)
	}

	repo, err := remote.NewRepository(parsedRef.Context().Name())
	if err != nil {
		return nil, fmt.Errorf("creating ORAS repository: %w", err)
	}

	keychain := s.client.AuthenticatorWrapper()

	authClient := &auth.Client{
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
	
	repo.Client = authClient

	return repo, nil
}

// PushArtifact pushes a directory as an OCI artifact to the registry.
func (s *ArtifactStore) PushArtifact(ctx context.Context, ref string, artifactType ArtifactType, path string, metadata map[string]string) error {
	repo, err := s.getORASRepository(ctx, ref)
	if err != nil {
		return err
	}
	
	parsedRef, err := name.ParseReference(ref)
	if err != nil {
		return err
	}
	tag := parsedRef.Identifier()

	fs, err := file.New(path)
	if err != nil {
		return fmt.Errorf("creating file store: %w", err)
	}
	defer fs.Close()

	//lint:ignore SA1019 We use oras.Pack for compatibility
	desc, err := oras.Pack(ctx, fs, string(artifactType), nil, oras.PackOptions{
		PackImageManifest: true,
	})
	if err != nil {
		return fmt.Errorf("packing artifact: %w", err)
	}

	_, err = oras.Copy(ctx, fs, desc.Digest.String(), repo, tag, oras.DefaultCopyOptions)
	if err != nil {
		return fmt.Errorf("copying artifact to registry: %w", err)
	}

	return nil
}

// PullArtifact pulls an OCI artifact from the registry to a local directory.
func (s *ArtifactStore) PullArtifact(ctx context.Context, ref string, destDir string) (ArtifactInfo, error) {
	repo, err := s.getORASRepository(ctx, ref)
	if err != nil {
		return ArtifactInfo{}, err
	}

	parsedRef, err := name.ParseReference(ref)
	if err != nil {
		return ArtifactInfo{}, err
	}

	fs, err := file.New(destDir)
	if err != nil {
		return ArtifactInfo{}, fmt.Errorf("creating file store: %w", err)
	}
	defer fs.Close()

	desc, err := oras.Copy(ctx, repo, parsedRef.Identifier(), fs, parsedRef.Identifier(), oras.DefaultCopyOptions)
	if err != nil {
		return ArtifactInfo{}, fmt.Errorf("pulling artifact: %w", err)
	}

	return ArtifactInfo{
		Digest: desc.Digest.String(),
		Tag:    parsedRef.Identifier(),
	}, nil
}

// ListArtifacts lists artifacts of a specific type in a repository.
func (s *ArtifactStore) ListArtifacts(ctx context.Context, repoRef string, artifactType ArtifactType) ([]ArtifactInfo, error) {
	tags, err := s.client.ListTags(ctx, repoRef)
	if err != nil {
		return nil, err
	}

	var infos []ArtifactInfo
	for _, t := range tags {
		infos = append(infos, ArtifactInfo{
			Tag: t,
		})
	}

	return infos, nil
}
