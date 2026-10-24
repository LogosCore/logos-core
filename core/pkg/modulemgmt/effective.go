// Package modulemgmt implements module management: the desired config core
// holds for a module's declared properties (two scopes, merged into an
// effective config), pushing it to instances over mgmt.config.apply, and
// invoking declared actions over mgmt.action.invoke. Wire types live in
// logos-golang-protocol/protocol/management.go; storage in
// models/module_management.go.
//
// It is transport-agnostic: the broker is behind messaging.RPCCaller and every
// store behind a narrow interface, so it unit-tests without either.
package modulemgmt

import (
	"encoding/json"
	"math"
	"reflect"

	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// Where a property's effective value comes from.
const (
	SourceDefault  = "default"
	SourceModule   = "module"
	SourceInstance = "instance"
)

// Sync states, matching the GraphQL ModuleSyncState enum.
const (
	SyncInSync    = "IN_SYNC"
	SyncPending   = "PENDING"
	SyncOutOfSync = "OUT_OF_SYNC"
)

// PropertyState is one declared property with its stored and effective value.
type PropertyState struct {
	Spec          protocol.PropertySpec
	ModuleValue   any
	HasModule     bool
	InstanceValue any
	HasInstance   bool
	// Effective is instance ?? module ?? the manifest default; Source says
	// which. For SourceDefault the module uses its own default — core never
	// sends a value for it.
	Effective any
	Source    string
}

// IsSet reports whether a value is stored at either scope.
func (p PropertyState) IsSet() bool { return p.HasModule || p.HasInstance }

// EffectiveConfig is an instance's merged desired config.
type EffectiveConfig struct {
	ModuleRevision   int64
	InstanceRevision int64
	Properties       []PropertyState
}

// Revision is the instance's desired revision: the sum of both scopes'
// revisions, so it grows whenever either scope is written.
func (e EffectiveConfig) Revision() int64 { return e.ModuleRevision + e.InstanceRevision }

// Wire is the config handed to the module: only properties that are declared
// in this instance's manifest AND have a stored value. An absent key tells the
// module to use its own default.
func (e EffectiveConfig) Wire() protocol.ManagementConfig {
	values := map[string]any{}
	for _, p := range e.Properties {
		if p.Source != SourceDefault {
			values[p.Spec.Key] = p.Effective
		}
	}
	return protocol.ManagementConfig{Revision: e.Revision(), Values: values}
}

// ComputeEffective merges the two scope documents (either may be nil) over a
// manifest. Stored keys the manifest no longer declares are ignored.
func ComputeEffective(manifest *protocol.ManagementManifest, mod, inst *models.ModuleConfig) EffectiveConfig {
	var e EffectiveConfig
	var modVals, instVals map[string]any
	if mod != nil {
		e.ModuleRevision = mod.Revision
		modVals = mod.Values
	}
	if inst != nil {
		e.InstanceRevision = inst.Revision
		instVals = inst.Values
	}
	if manifest == nil {
		return e
	}
	e.Properties = make([]PropertyState, 0, len(manifest.Properties))
	for _, spec := range manifest.Properties {
		p := PropertyState{Spec: spec, Effective: spec.Default, Source: SourceDefault}
		if v, ok := modVals[spec.Key]; ok && v != nil {
			p.ModuleValue, p.HasModule = v, true
			p.Effective, p.Source = v, SourceModule
		}
		if v, ok := instVals[spec.Key]; ok && v != nil {
			p.InstanceValue, p.HasInstance = v, true
			p.Effective, p.Source = v, SourceInstance
		}
		e.Properties = append(e.Properties, p)
	}
	return e
}

// SyncState derives the instance's sync state from its registry row and its
// desired revision.
//
// An instance that has processed the desired revision is IN_SYNC even if an
// earlier push recorded an error — the usual case is a reply that was lost
// after the module applied, which the next heartbeat then reports as applied.
func SyncState(mod models.Module, revision int64) string {
	switch {
	case mod.Status != models.ModuleStatusRegistered:
		return SyncOutOfSync
	case mod.AppliedRevision == revision:
		return SyncInSync
	case mod.LastApplyError != "":
		return SyncOutOfSync
	default:
		return SyncPending
	}
}

// normalizeValue converts a decoded JSON value to the canonical form stored and
// compared: every number becomes float64 (json.Number, Go ints), lists become
// []any. Anything else is returned as is.
func normalizeValue(v any) any {
	switch n := v.(type) {
	case json.Number:
		if f, err := n.Float64(); err == nil {
			return f
		}
		return n.String()
	case int:
		return float64(n)
	case int32:
		return float64(n)
	case int64:
		return float64(n)
	case float32:
		return float64(n)
	case []string:
		out := make([]any, len(n))
		for i, s := range n {
			out[i] = s
		}
		return out
	case []any:
		out := make([]any, len(n))
		for i, it := range n {
			out[i] = normalizeValue(it)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(n))
		for k, it := range n {
			out[k] = normalizeValue(it)
		}
		return out
	}
	return v
}

// valuesEqual compares two normalized JSON values.
func valuesEqual(a, b any) bool {
	if fa, ok := a.(float64); ok {
		if fb, ok := b.(float64); ok {
			return fa == fb || (math.IsNaN(fa) && math.IsNaN(fb))
		}
	}
	return reflect.DeepEqual(normalizeValue(a), normalizeValue(b))
}

// redact replaces a secret value for the audit trail / stored params.
func redact(spec protocol.FieldSpec, v any) any {
	if spec.Type == protocol.FieldSecret && v != nil {
		return models.RedactedValue
	}
	return v
}
