package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
)

// Authenticator provides authentication for OCI registries.
type Authenticator interface {
	Resolve(registry string) (authn.Authenticator, error)
}

// ChainAuthenticator tries multiple authentication methods in order.
type ChainAuthenticator struct {
	Methods []Authenticator
}

func (c *ChainAuthenticator) Resolve(registry string) (authn.Authenticator, error) {
	for _, m := range c.Methods {
		auth, err := m.Resolve(registry)
		if err == nil && auth != authn.Anonymous {
			return auth, nil
		}
	}
	return authn.Anonymous, nil
}

// EnvAuthenticator reads credentials from environment variables.
type EnvAuthenticator struct{}

func (e *EnvAuthenticator) Resolve(registry string) (authn.Authenticator, error) {
	if token := os.Getenv("AGENTBOX_GITHUB_TOKEN"); token != "" {
		return &authn.Bearer{Token: token}, nil
	}
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return &authn.Bearer{Token: token}, nil
	}
	return authn.Anonymous, nil
}

// DefaultKeychainAuthenticator uses the default docker keychain.
type DefaultKeychainAuthenticator struct{}

func (d *DefaultKeychainAuthenticator) Resolve(registry string) (authn.Authenticator, error) {
	reg, err := name.NewRegistry(registry)
	if err != nil {
		return authn.Anonymous, err
	}
	return authn.DefaultKeychain.Resolve(reg)
}

// FileAuthenticator reads credentials from ~/.agentbox/credentials.json
type FileAuthenticator struct{}

type credentialsFile struct {
	Registries map[string]struct {
		Token    string `json:"token,omitempty"`
		Username string `json:"username,omitempty"`
		Password string `json:"password,omitempty"`
	} `json:"registries"`
}

func (f *FileAuthenticator) Resolve(registry string) (authn.Authenticator, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return authn.Anonymous, nil
	}
	
	path := filepath.Join(home, ".agentbox", "credentials.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return authn.Anonymous, nil // Missing file is fine
	}
	
	var creds credentialsFile
	if err := json.Unmarshal(data, &creds); err != nil {
		return authn.Anonymous, fmt.Errorf("parsing credentials file: %w", err)
	}
	
	if regCreds, ok := creds.Registries[registry]; ok {
		if regCreds.Token != "" {
			return &authn.Bearer{Token: regCreds.Token}, nil
		}
		if regCreds.Username != "" && regCreds.Password != "" {
			return &authn.Basic{
				Username: regCreds.Username,
				Password: regCreds.Password,
			}, nil
		}
	}
	
	return authn.Anonymous, nil
}

// DefaultAuthenticator returns a ChainAuthenticator with the default methods.
func DefaultAuthenticator() Authenticator {
	return &ChainAuthenticator{
		Methods: []Authenticator{
			&EnvAuthenticator{},
			&FileAuthenticator{},
			&DefaultKeychainAuthenticator{},
		},
	}
}

// ContainerRegistryAuthWrapper wraps our Authenticator to implement authn.Keychain
type ContainerRegistryAuthWrapper struct {
	Auth Authenticator
}

func (w *ContainerRegistryAuthWrapper) Resolve(target authn.Resource) (authn.Authenticator, error) {
	return w.Auth.Resolve(target.RegistryStr())
}
