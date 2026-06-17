package ocx

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
)

// ResolvedComponent is a fetched OCX component ready to be consumed by the
// normalizer. StagingDir contains the extracted files and must be cleaned up
// by the caller (typically via ResolvedSet.Cleanup).
type ResolvedComponent struct {
	Manifest   *ComponentManifest
	Source     string
	StagingDir string
}

// ResolvedSet holds all resolved OCX components in dependency order plus the
// deep-merged opencode configuration.
type ResolvedSet struct {
	Components []*ResolvedComponent
	Opencode   OpencodeBlock
}

// Cleanup removes all temporary staging directories created during resolution.
func (r *ResolvedSet) Cleanup() {
	if r == nil {
		return
	}
	for _, c := range r.Components {
		if c != nil && c.StagingDir != "" {
			os.RemoveAll(c.StagingDir)
		}
	}
}

// Fetcher is the minimal interface required by Resolver.
type Fetcher interface {
	Fetch(ctx context.Context, ref string) (*ComponentManifest, string, error)
}

// Resolver resolves OCX component references, recursively fetches their
// dependencies, and deep-merges opencode blocks.
type Resolver struct {
	fetcher Fetcher
}

// NewResolver creates a new Resolver backed by fetcher.
func NewResolver(fetcher Fetcher) *Resolver {
	return &Resolver{fetcher: fetcher}
}

// Resolve fetches the components identified by refs and their transitive
// dependencies. refs are treated as full OCI references. The returned set is
// ordered so that dependencies appear before dependents.
func (r *Resolver) Resolve(ctx context.Context, refs []string) (*ResolvedSet, error) {
	if r.fetcher == nil {
		return nil, fmt.Errorf("fetcher is required")
	}

	state := &resolveState{
		bySource: make(map[string]*ResolvedComponent),
		order:    make([]string, 0, len(refs)),
	}

	for _, ref := range refs {
		if ref == "" {
			continue
		}
		if err := r.resolve(ctx, state, ref); err != nil {
			return nil, err
		}
	}

	set := &ResolvedSet{
		Components: make([]*ResolvedComponent, 0, len(state.order)),
	}
	for _, source := range state.order {
		set.Components = append(set.Components, state.bySource[source])
	}

	set.Opencode = r.mergeOpencodeInOrder(set.Components)
	return set, nil
}

type resolveState struct {
	visiting map[string]struct{}
	bySource map[string]*ResolvedComponent
	order    []string
}

func (r *Resolver) resolve(ctx context.Context, state *resolveState, ref string) error {
	if state.visiting == nil {
		state.visiting = make(map[string]struct{})
	}
	if _, ok := state.visiting[ref]; ok {
		return fmt.Errorf("cyclic OCX dependency detected at %q", ref)
	}
	if _, ok := state.bySource[ref]; ok {
		return nil
	}

	state.visiting[ref] = struct{}{}
	manifest, staging, err := r.fetcher.Fetch(ctx, ref)
	if err != nil {
		delete(state.visiting, ref)
		return fmt.Errorf("fetching %q: %w", ref, err)
	}

	component := &ResolvedComponent{
		Manifest:   manifest,
		Source:     ref,
		StagingDir: staging,
	}
	state.bySource[ref] = component

	for _, dep := range manifest.Dependencies {
		depRef := normalizeDependencyRef(dep, ref)
		if err := r.resolve(ctx, state, depRef); err != nil {
			delete(state.visiting, ref)
			return err
		}
	}

	delete(state.visiting, ref)
	state.order = append(state.order, ref)
	return nil
}

// normalizeDependencyRef converts an OCX dependency shorthand into a full OCI
// reference. If the dependency is already a full OCI reference it is returned
// unchanged. Bare names are resolved relative to the parent component's
// repository using the same tag.
func normalizeDependencyRef(dep, parentRef string) string {
	if strings.Contains(dep, "://") || strings.Contains(dep, "/") && strings.Contains(dep, ":") {
		return dep
	}
	idx := strings.LastIndex(parentRef, "/")
	if idx < 0 {
		return dep
	}
	repo := parentRef[:idx]
	return fmt.Sprintf("%s/%s", repo, dep)
}

func (r *Resolver) mergeOpencodeInOrder(components []*ResolvedComponent) OpencodeBlock {
	var merged OpencodeBlock
	for _, c := range components {
		if c == nil || c.Manifest == nil || len(c.Manifest.Opencode) == 0 {
			continue
		}
		merged = mergeOpencode(merged, c.Manifest.Opencode)
	}
	return merged
}

// mergeOpencode deep-merges src into dst following OCX semantics:
//   - maps are merged recursively
//   - arrays under "plugin" are concatenated and deduplicated
//   - arrays under "instructions" are concatenated and deduplicated
//   - all other arrays are replaced by src
func mergeOpencode(dst, src OpencodeBlock) OpencodeBlock {
	if dst == nil {
		dst = make(OpencodeBlock)
	}
	if src == nil {
		return dst
	}
	for key, srcVal := range src {
		dst[key] = mergeOpencodeValue(dst[key], srcVal, key)
	}
	return dst
}

func mergeOpencodeValue(dstVal, srcVal interface{}, key string) interface{} {
	srcMap, srcIsMap := srcVal.(map[string]interface{})
	dstMap, dstIsMap := dstVal.(map[string]interface{})
	if srcIsMap && dstIsMap {
		return mergeOpencode(dstMap, srcMap)
	}

	srcSlice, srcIsSlice := srcVal.([]interface{})
	dstSlice, dstIsSlice := dstVal.([]interface{})
	if srcIsSlice && dstIsSlice {
		switch key {
		case "plugin":
			return mergePluginSlice(dstSlice, srcSlice)
		case "instructions":
			return deduplicateStringSlice(dstSlice, srcSlice)
		default:
			return srcVal
		}
	}

	return srcVal
}

func mergePluginSlice(dst, src []interface{}) []interface{} {
	seen := make(map[string]int, len(dst)+len(src))
	out := make([]interface{}, 0, len(dst)+len(src))

	process := func(items []interface{}) {
		for _, item := range items {
			s, ok := item.(string)
			if !ok {
				continue
			}
			canonical := canonicalPluginName(s)
			if idx, exists := seen[canonical]; exists {
				out[idx] = s
				continue
			}
			seen[canonical] = len(out)
			out = append(out, s)
		}
	}

	process(dst)
	process(src)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].(string) < out[j].(string)
	})
	return out
}

func deduplicateStringSlice(dst, src []interface{}) []interface{} {
	seen := make(map[string]struct{}, len(dst)+len(src))
	out := make([]interface{}, 0, len(dst)+len(src))

	process := func(items []interface{}) {
		for _, item := range items {
			s, ok := item.(string)
			if !ok {
				continue
			}
			if _, exists := seen[s]; exists {
				continue
			}
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}

	process(dst)
	process(src)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].(string) < out[j].(string)
	})
	return out
}

// canonicalPluginName strips an optional npm version suffix so that
// "pkg@1.2.3" and "pkg@1.2.4" are considered the same package for
// deduplication.
func canonicalPluginName(s string) string {
	if idx := strings.Index(s, "@"); idx > 0 {
		return s[:idx]
	}
	return s
}
