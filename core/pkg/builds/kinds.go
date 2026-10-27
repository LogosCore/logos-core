package builds

import (
	"context"
	"fmt"
	"sort"

	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// Kind is a factory kind and its current build schema: the highest version
// any of its instances declared. Instances counts the registered instances
// declaring that version; a build needs one. Icon is empty when none of its
// instances sent one.
type Kind struct {
	ModuleName string
	Schema     protocol.BuildSchema
	Instances  int
	Icon       string
	IconLight  string // the icon on light themes; empty when Icon serves both
}

// Kinds returns every factory kind that declared a build schema, by name.
func (s *Service) Kinds(ctx context.Context) ([]Kind, error) {
	schemas, err := s.registry.LatestBuildSchemas(ctx)
	if err != nil {
		return nil, fmt.Errorf("load build schemas: %w", err)
	}
	active, err := s.registry.ListActive(ctx)
	if err != nil {
		return nil, fmt.Errorf("list registered modules: %w", err)
	}
	icons, err := s.registry.LatestIcons(ctx, protocol.ServiceFactory)
	if err != nil {
		return nil, fmt.Errorf("load factory icons: %w", err)
	}
	out := make([]Kind, 0, len(schemas))
	for name, sc := range schemas {
		out = append(out, Kind{ModuleName: name, Schema: sc.BuildSchema, Instances: len(declaring(active, name, sc.Version)),
			Icon: icons[name].Icon, IconLight: icons[name].IconLight})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModuleName < out[j].ModuleName })
	return out, nil
}

// schema returns a factory kind's current build schema; nil when no instance
// of it declared one.
func (s *Service) schema(ctx context.Context, moduleName string) (*protocol.BuildSchema, error) {
	sc, err := s.registry.LatestBuildSchema(ctx, moduleName)
	if err != nil {
		return nil, fmt.Errorf("load build schema: %w", err)
	}
	if sc == nil {
		return nil, nil
	}
	return &sc.BuildSchema, nil
}

// instances returns the registered instances of a factory kind that declare
// build schema version.
func (s *Service) instances(ctx context.Context, moduleName string, version int) ([]models.Module, error) {
	active, err := s.registry.ListActive(ctx)
	if err != nil {
		return nil, fmt.Errorf("list registered modules: %w", err)
	}
	return declaring(active, moduleName, version), nil
}

func declaring(mods []models.Module, moduleName string, version int) []models.Module {
	var out []models.Module
	for _, m := range mods {
		if m.Type == protocol.ServiceFactory && m.Name == moduleName && m.Build != nil && m.Build.Version == version && m.RPCQueue != "" {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Instance < out[j].Instance })
	return out
}
