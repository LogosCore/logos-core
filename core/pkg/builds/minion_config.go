package builds

import (
	"context"
	"fmt"

	"github.com/logoscore/logos-golang-protocol/protocol"
)

// Unconfigurable is a build whose minion config cannot be made (docs:
// minion-build, The minion config): a channel that numbers no fields of the
// build's connection kind, or one whose schema is at another version than
// the build's connections follow. No retry makes it: the build fails, or is
// refused as it is made.
type Unconfigurable struct{ Why string }

func (e *Unconfigurable) Error() string { return e.Why }

// minionConfig is the minion config of req, a request of schema, whose
// factory asks for one: the request's settings numbered, with the connection
// schemas of the channels it connects through, at the versions its
// connections follow. What the factory does with it is its own business.
func (s *Service) minionConfig(ctx context.Context, schema protocol.BuildSchema, req protocol.BuildRequest) ([]byte, error) {
	channels := map[string]*protocol.ConnectionSchema{}
	for _, c := range req.Connections {
		module := c.Endpoint.Channel.ModuleName
		if _, ok := channels[module]; ok {
			continue
		}
		cs, err := s.channels.Schema(ctx, module)
		if err != nil {
			return nil, fmt.Errorf("load channel %q's connection schema: %w", module, err)
		}
		channels[module] = cs
	}
	at := func(module string, version int) (protocol.ConnectionSchema, bool) {
		cs := channels[module]
		if cs == nil || cs.Version != version {
			return protocol.ConnectionSchema{}, false
		}
		return *cs, true
	}
	cfg, err := protocol.EncodeMinionConfig(schema, req, at)
	if err != nil {
		return nil, &Unconfigurable{Why: "the minion config: " + err.Error()}
	}
	return cfg, nil
}
