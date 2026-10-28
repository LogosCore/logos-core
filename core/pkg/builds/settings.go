package builds

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// ConnectionInput is one connection of a build: a channel kind,
// its connection settings (by the channel's connection schema; a field without
// a value takes its default) and the factory's connection parameter values
// for it. Name is given to the minion with the settings and is unique among
// the connections; empty, the connection is called <channel>-<position>.
// Description is the operators' note on it: core keeps it, and never puts it
// in a build request, so it cannot end up in an artifact.
type ConnectionInput struct {
	ModuleName  string
	Name        string
	Description string
	Settings    map[string]any
	Values      map[string]any
}

// MaxDescriptionLength bounds a build's and a connection's description, in
// characters.
const MaxDescriptionLength = 500

// connectionNamePattern: a connection is named like an endpoint.
var connectionNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)

// compactConnections trims names and leaves empty values out, as compact does
// for a build's values.
func compactConnections(in []ConnectionInput) []ConnectionInput {
	out := make([]ConnectionInput, len(in))
	for i, c := range in {
		out[i] = ConnectionInput{
			ModuleName: strings.TrimSpace(c.ModuleName), Name: strings.TrimSpace(c.Name),
			Description: strings.TrimSpace(c.Description), Settings: compact(c.Settings), Values: compact(c.Values),
		}
	}
	return out
}

// connectionName is a connection's name, or its default.
func connectionName(c ConnectionInput, i int) string {
	if c.Name != "" {
		return c.Name
	}
	return fmt.Sprintf("%s-%d", c.ModuleName, i+1)
}

// settings are what a build is made of.
type settings struct {
	Target   string
	Platform string
	Values   map[string]any
	// Commands are the ones picked, by id; nil for the target's defaults.
	Commands    []string
	Connections []ConnectionInput
}

// checked is what check learned about valid settings.
type checked struct {
	target protocol.BuildTarget
	fields []protocol.FieldSpec // the target's, shared ones first
	// commands are the build's: the ones picked and the target's required
	// ones, in the schema's order.
	commands []string
	// channels are the connections' channel schemas, by connection index.
	channels []*protocol.ConnectionSchema
	// fileIDs are the build files the values and connection values name.
	fileIDs []uuid.UUID
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

// check validates settings for operation op against schema. Problems come
// back in out; err is for failures to check.
func (s *Service) check(ctx context.Context, op uuid.UUID, schema protocol.BuildSchema, st settings) (c checked, out problems, err error) {
	out = problems{}
	t, ok := schema.Target(st.Target)
	if !ok {
		out.add("target", "not a target of this factory")
		return c, out, nil
	}
	c.target, c.fields = t, schema.TargetFields(t)
	switch {
	case st.Platform == "":
		out.add("platform", "a platform is required")
	case st.Platform != "" && !t.HasPlatform(st.Platform):
		out.add("platform", "not a platform of target %q", t.ID)
	}

	files := map[uuid.UUID]bool{}
	if err := s.checkValues(ctx, op, "values.", c.fields, st.Values, out, files); err != nil {
		return c, out, err
	}
	c.commands = commandsFor(schema, t, st.Commands, out)

	if len(st.Connections) == 0 {
		out.add("connections", "at least one connection is required")
	}
	c.channels = make([]*protocol.ConnectionSchema, len(st.Connections))
	named := map[string]int{} // connection index by name
	for i, conn := range st.Connections {
		key := fmt.Sprintf("connections[%d]", i)
		name := connectionName(conn, i)
		j, taken := named[name]
		switch {
		case conn.Name != "" && !connectionNamePattern.MatchString(conn.Name):
			out.add(key+".name", "must match %s", connectionNamePattern)
		case taken:
			out.add(key+".name", "connection %d is called %q too", j+1, name)
		default:
			named[name] = i
		}
		if utf8.RuneCountInString(conn.Description) > MaxDescriptionLength {
			out.add(key+".description", "longer than %d characters", MaxDescriptionLength)
		}
		if conn.ModuleName == "" {
			out.add(key, "a channel is required")
			continue
		}
		cs, err := s.channels.Schema(ctx, conn.ModuleName)
		if err != nil {
			return c, out, err
		}
		if cs == nil {
			out.add(key, "no %q channel has declared minion connection settings", conn.ModuleName)
			continue
		}
		c.channels[i] = cs
		if !schema.Supports(t, conn.ModuleName, cs.Version) {
			out.add(key, "target %q cannot connect through channel %q version %d", t.ID, conn.ModuleName, cs.Version)
			continue
		}
		if err := s.checkValues(ctx, op, key+".settings.", cs.Fields, conn.Settings, out, files); err != nil {
			return c, out, err
		}
		ch, _ := schema.Channel(conn.ModuleName)
		if err := s.checkValues(ctx, op, key+".values.", ch.Fields, conn.Values, out, files); err != nil {
			return c, out, err
		}
	}
	c.fileIDs = slices.SortedFunc(maps.Keys(files), func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	return c, out, nil
}

// commandsFor resolves a build's commands for target t: the ones picked, or
// the target's defaults when none are named (nil), with its required ones
// always, in the schema's order. Each one picked must be a command of t,
// picked once.
func commandsFor(schema protocol.BuildSchema, t protocol.BuildTarget, picked []string, out problems) []string {
	if picked == nil {
		return schema.DefaultCommands(t)
	}
	seen := make(map[string]bool, len(picked))
	for i, id := range picked {
		key := fmt.Sprintf("commands[%d]", i)
		c, ok := schema.Command(id)
		switch {
		case !ok:
			out.add(key, "%q is not a command of this factory", id)
		case !c.For(t.ID):
			out.add(key, "%q is not a command of target %q", id, t.ID)
		case seen[id]:
			out.add(key, "%q is picked twice", id)
		}
		seen[id] = true
	}
	commands := []string{}
	for _, c := range schema.TargetCommands(t) {
		if c.Required || seen[c.ID] {
			commands = append(commands, c.ID)
		}
	}
	return commands
}

// checkValues checks values against fields: no undeclared keys, every value
// valid, required ones set, and file values naming a fitting build file of
// op, whose ids go into files.
func (s *Service) checkValues(ctx context.Context, op uuid.UUID, prefix string, fields []protocol.FieldSpec, values map[string]any, out problems, files map[uuid.UUID]bool) error {
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
			if f.Required && f.Default == nil {
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
		files[file.FileID] = true
	}
	return nil
}

// fileOf returns a build file of operation op, by the id a value names it
// by.
func (s *Service) fileOf(ctx context.Context, op uuid.UUID, id string) (*models.BuildFile, error) {
	fid, err := uuid.Parse(id)
	if err != nil {
		return nil, ErrFileNotFound
	}
	return s.file(ctx, op, fid)
}

// file returns a build file of operation op.
func (s *Service) file(ctx context.Context, op, fid uuid.UUID) (*models.BuildFile, error) {
	f, err := s.repos.Files.FindByFileID(ctx, fid)
	if repository.IsNotFound(err) || (err == nil && f.OperationID != op) {
		return nil, ErrFileNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load build file: %w", err)
	}
	return f, nil
}

// compact drops values that leave a field unset: null, "" and [].
func compact(values map[string]any) map[string]any {
	out := make(map[string]any, len(values))
	for k, v := range values {
		switch x := v.(type) {
		case nil:
			continue
		case string:
			if x == "" {
				continue
			}
		case []any:
			if len(x) == 0 {
				continue
			}
		}
		out[k] = v
	}
	return out
}
