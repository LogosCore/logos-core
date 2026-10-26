package builds

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/channelendpoints"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// ConnectionInput is one endpoint of a build or preset, by id, with the
// factory's connection parameter values for it.
type ConnectionInput struct {
	EndpointID string
	Values     map[string]any
}

// settings are what a build is made of, and what a preset holds of it.
type settings struct {
	Target      string
	Platform    string
	Values      map[string]any
	Connections []ConnectionInput
}

// checked is what check learned about valid settings.
type checked struct {
	target protocol.BuildTarget
	fields []protocol.FieldSpec // the target's, shared ones first
	// endpoints by connection index.
	endpoints []channelendpoints.View
	// fileIDs are the build files the values and connection values name.
	fileIDs []string
}

// problems collects validation problems by where they are.
type problems map[string]string

func (p problems) add(key, format string, args ...any) { p[key] = fmt.Sprintf(format, args...) }

func (p problems) err(message string) error {
	if len(p) == 0 {
		return nil
	}
	return &ValidationError{Message: message, Problems: p}
}

// check validates settings for operation op against schema. A preset is
// partial: the platform, the connections and required values may be left for
// the build form. Problems come back in out; err is for failures to check.
func (s *Service) check(ctx context.Context, op uuid.UUID, schema protocol.BuildSchema, st settings, partial bool) (c checked, out problems, err error) {
	out = problems{}
	t, ok := schema.Target(st.Target)
	if !ok {
		out.add("target", "not a target of this factory")
		return c, out, nil
	}
	c.target, c.fields = t, schema.TargetFields(t)
	switch {
	case st.Platform == "" && !partial:
		out.add("platform", "a platform is required")
	case st.Platform != "" && !t.HasPlatform(st.Platform):
		out.add("platform", "not a platform of target %q", t.ID)
	}

	files := map[string]bool{}
	if err := s.checkValues(ctx, op, "values.", c.fields, st.Values, partial, out, files); err != nil {
		return c, out, err
	}

	if len(st.Connections) == 0 && !partial {
		out.add("connections", "at least one endpoint is required")
	}
	c.endpoints = make([]channelendpoints.View, len(st.Connections))
	for i, conn := range st.Connections {
		key := fmt.Sprintf("connections[%d]", i)
		v, err := s.endpoints.Get(ctx, conn.EndpointID)
		switch {
		case errors.Is(err, channelendpoints.ErrNotFound) || (err == nil && v.OperationID != op):
			out.add(key, "no such endpoint in this operation")
			continue
		case err != nil:
			return c, out, fmt.Errorf("load endpoint: %w", err)
		}
		c.endpoints[i] = v
		switch {
		case len(v.Problems) > 0 || v.Schema == nil:
			out.add(key, "endpoint %q cannot be used: it does not fit its channel's current schema", v.Name)
			continue
		case !schema.Supports(t, v.ModuleName, v.Schema.Version):
			out.add(key, "target %q cannot connect through channel %q version %d", t.ID, v.ModuleName, v.Schema.Version)
			continue
		}
		ch, _ := schema.Channel(v.ModuleName)
		if err := s.checkValues(ctx, op, key+".values.", ch.Fields, conn.Values, partial, out, files); err != nil {
			return c, out, err
		}
	}
	for id := range files {
		c.fileIDs = append(c.fileIDs, id)
	}
	sort.Strings(c.fileIDs)
	return c, out, nil
}

// checkValues checks values against fields: no undeclared keys, every value
// valid, required ones set unless partial, and file values naming a fitting
// build file of op, whose ids go into files.
func (s *Service) checkValues(ctx context.Context, op uuid.UUID, prefix string, fields []protocol.FieldSpec, values map[string]any, partial bool, out problems, files map[string]bool) error {
	declared := make(map[string]protocol.FieldSpec, len(fields))
	for _, f := range fields {
		declared[f.Key] = f
	}
	for key := range values {
		if _, ok := declared[key]; !ok {
			out.add(prefix+key, "not a declared field")
		}
	}
	for _, f := range fields {
		v, ok := values[f.Key]
		if !ok || v == nil {
			if f.Required && f.Default == nil && !partial {
				out.add(prefix+f.Key, "value is required")
			}
			continue
		}
		if err := protocol.ValidateValue(f, v); err != nil {
			out.add(prefix+f.Key, "%v", err)
			continue
		}
		if f.Type != protocol.FieldFile {
			continue
		}
		id := v.(string)
		file, err := s.fileOf(ctx, op, id)
		if errors.Is(err, ErrFileNotFound) {
			out.add(prefix+f.Key, "no such build file in this operation")
			continue
		}
		if err != nil {
			return err
		}
		if err := protocol.ValidateFile(f, file.Protocol()); err != nil {
			out.add(prefix+f.Key, "%s: %v", file.Name, err)
			continue
		}
		files[id] = true
	}
	return nil
}

// fileOf returns a build file of operation op.
func (s *Service) fileOf(ctx context.Context, op uuid.UUID, id string) (*models.BuildFile, error) {
	fid, err := uuid.Parse(id)
	if err != nil {
		return nil, ErrFileNotFound
	}
	f, err := s.repos.Files.FindByFileID(ctx, fid)
	if repository.IsNotFound(err) || (err == nil && f.OperationID != op) {
		return nil, ErrFileNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load build file: %w", err)
	}
	return f, nil
}
