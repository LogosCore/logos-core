package resolver

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/modulelogs"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// ModuleLogStreamer opens a live log tail (modulelogs.Relay).
type ModuleLogStreamer interface {
	Stream(ctx context.Context, req modulelogs.Request) (<-chan *modulelogs.Event, error)
}

// ModuleLogs is the moduleLogs subscription: a live tail relayed from the
// module for as long as the subscription is open. Like management, it is
// operator-only: agent keys are refused.
func (r *moduleResolver) ModuleLogs(ctx context.Context, instance string, minLevel *model.ModuleLogLevel) (<-chan *model.ModuleLogEvent, error) {
	auth, err := refuseAgent(ctx)
	if err != nil {
		return nil, err
	}
	if r.logs == nil {
		return nil, errors.New("module logs are not available")
	}
	level := protocol.LogInfo
	if minLevel != nil {
		level = protocol.LogLevel(strings.ToLower(string(*minLevel)))
	}
	events, err := r.logs.Stream(ctx, modulelogs.Request{
		Instance: strings.TrimSpace(instance),
		MinLevel: level,
		Actor:    managementActor(auth),
	})
	switch {
	case errors.Is(err, modulelogs.ErrModuleNotFound):
		return nil, moduleGQLError(ctx, errCodeNotFound, "module not found")
	case errors.Is(err, modulelogs.ErrNotSupported), errors.Is(err, modulelogs.ErrNotRegistered):
		return nil, moduleGQLError(ctx, errCodeBadUserInput, err.Error())
	case err != nil:
		return nil, err
	}

	// Never closed: gqlgen stops reading when ctx ends (see stream in
	// graphql/resolver/subscription_helpers.go).
	out := make(chan *model.ModuleLogEvent, 1)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case ev := <-events:
				select {
				case out <- toLogEventModel(ev):
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

func toLogEventModel(ev *modulelogs.Event) *model.ModuleLogEvent {
	recs := make([]*model.ModuleLogRecord, len(ev.Records))
	for i, rec := range ev.Records {
		m := &model.ModuleLogRecord{
			Time:    rec.Time.UTC().Format(time.RFC3339Nano),
			Level:   model.ModuleLogLevel(strings.ToUpper(string(rec.Level))),
			Message: rec.Message,
		}
		if !m.Level.IsValid() {
			m.Level = model.ModuleLogLevelInfo
		}
		if len(rec.Attrs) > 0 {
			m.Attrs = rec.Attrs
		}
		recs[i] = m
	}
	out := &model.ModuleLogEvent{
		Records: recs,
		Backlog: ev.Backlog,
		Dropped: int(ev.Dropped),
		Lost:    ev.Lost,
		Reason:  strPtr(ev.Reason),
	}
	if ev.State != "" {
		st := model.ModuleLogStreamState(ev.State)
		out.State = &st
	}
	return out
}
