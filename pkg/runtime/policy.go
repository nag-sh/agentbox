package runtime

// PolicyLabel is the image label key used to persist runtime policy (network
// flags and resource limits) so that `agentbox run` can apply them even when
// invoked with a pre-built image rather than a manifest.
const PolicyLabel = "dev.agentbox.runtime.policy"

// Policy captures the host-side runtime decisions baked into an image at build
// time. It is serialized into the image config label PolicyLabel.
type Policy struct {
	NetworkFlags []string `json:"networkFlags,omitempty"`
	CPUs         string   `json:"cpus,omitempty"`
	MemoryBytes  int64    `json:"memoryBytes,omitempty"`
	PidsLimit    int64    `json:"pidsLimit,omitempty"`
}
