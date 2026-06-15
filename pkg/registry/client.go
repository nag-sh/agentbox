package registry

import (
	"context"
	"fmt"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// ClientOptions configures the OCI registry client.
type ClientOptions struct {
	Authenticator Authenticator
}

// Client provides a high-level interface for OCI registry operations.
type Client struct {
	auth Authenticator
}

// NewClient creates a new OCI registry client.
func NewClient(opts ClientOptions) (*Client, error) {
	if opts.Authenticator == nil {
		opts.Authenticator = DefaultAuthenticator()
	}
	
	return &Client{
		auth: opts.Authenticator,
	}, nil
}

// PullImage pulls an OCI image from a registry.
func (c *Client) PullImage(ctx context.Context, ref string) (v1.Image, error) {
	parsedRef, err := name.ParseReference(ref)
	if err != nil {
		return nil, fmt.Errorf("parsing reference %q: %w", ref, err)
	}

	opts := []remote.Option{
		remote.WithAuthFromKeychain(&ContainerRegistryAuthWrapper{Auth: c.auth}),
		remote.WithContext(ctx),
	}

	img, err := remote.Image(parsedRef, opts...)
	if err != nil {
		return nil, fmt.Errorf("pulling image %q: %w", ref, err)
	}

	return img, nil
}

// PushImage pushes an OCI image to a registry.
func (c *Client) PushImage(ctx context.Context, ref string, img v1.Image) error {
	parsedRef, err := name.ParseReference(ref)
	if err != nil {
		return fmt.Errorf("parsing reference %q: %w", ref, err)
	}

	opts := []remote.Option{
		remote.WithAuthFromKeychain(&ContainerRegistryAuthWrapper{Auth: c.auth}),
		remote.WithContext(ctx),
	}

	if err := remote.Write(parsedRef, img, opts...); err != nil {
		return fmt.Errorf("pushing image %q: %w", ref, err)
	}

	return nil
}

// ResolveDigest resolves an OCI reference to its digest.
func (c *Client) ResolveDigest(ctx context.Context, ref string) (v1.Hash, error) {
	parsedRef, err := name.ParseReference(ref)
	if err != nil {
		return v1.Hash{}, fmt.Errorf("parsing reference %q: %w", ref, err)
	}

	opts := []remote.Option{
		remote.WithAuthFromKeychain(&ContainerRegistryAuthWrapper{Auth: c.auth}),
		remote.WithContext(ctx),
	}

	desc, err := remote.Head(parsedRef, opts...)
	if err != nil {
		return v1.Hash{}, fmt.Errorf("resolving digest for %q: %w", ref, err)
	}

	return desc.Digest, nil
}

// ListTags lists all tags in an OCI repository.
func (c *Client) ListTags(ctx context.Context, repo string) ([]string, error) {
	parsedRepo, err := name.NewRepository(repo)
	if err != nil {
		return nil, fmt.Errorf("parsing repository %q: %w", repo, err)
	}

	opts := []remote.Option{
		remote.WithAuthFromKeychain(&ContainerRegistryAuthWrapper{Auth: c.auth}),
		remote.WithContext(ctx),
	}

	tags, err := remote.List(parsedRepo, opts...)
	if err != nil {
		return nil, fmt.Errorf("listing tags for %q: %w", repo, err)
	}

	return tags, nil
}

// CopyImage copies an OCI image from one registry to another.
func (c *Client) CopyImage(ctx context.Context, src, dst string) error {
	srcRef, err := name.ParseReference(src)
	if err != nil {
		return fmt.Errorf("parsing source reference %q: %w", src, err)
	}
	
	dstRef, err := name.ParseReference(dst)
	if err != nil {
		return fmt.Errorf("parsing destination reference %q: %w", dst, err)
	}

	keychain := &ContainerRegistryAuthWrapper{Auth: c.auth}
	
	opt := crane.WithAuthFromKeychain(keychain)
	
	if err := crane.Copy(srcRef.String(), dstRef.String(), opt); err != nil {
		return fmt.Errorf("copying image from %q to %q: %w", src, dst, err)
	}

	return nil
}

// AuthenticatorWrapper exports the wrapper for external packages to use if needed
func (c *Client) AuthenticatorWrapper() authn.Keychain {
	return &ContainerRegistryAuthWrapper{Auth: c.auth}
}
