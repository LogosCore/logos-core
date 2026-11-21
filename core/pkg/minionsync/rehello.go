package minionsync

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/logoscore/logos-core/core/pkg/eventbus"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"go.uber.org/zap"
)

// Rehello (core's rehello command, models.CommandRehello): an operator asks
// a session's minion for its hello again. Core keeps the ask on the session
// (RehelloAt), the outbox sends a rehello item with each message until a
// hello comes, models.RehelloTries at most, and the rehello commands
// waiting end: done with what the hello said and what changed, failed when
// none came. A hello under the session answers it, and so does a new
// process's claiming the session.

// AskRehello asks the session's minion for its hello: kept, and sent with
// core's next message to it (a nudge: at once where it can go at once).
// session is updated in place.
func (s *Service) AskRehello(ctx context.Context, session *models.MinionSession) error {
	if session.Closed(s.now()) {
		return fmt.Errorf("the session is closed")
	}
	now := s.now()
	if err := s.sessions.AskRehello(ctx, session.SessionID, now); err != nil {
		return fmt.Errorf("ask for a hello: %w", err)
	}
	session.RehelloAt, session.RehelloSends = &now, 0
	s.Nudge(session.SessionID, false)
	return nil
}

// rehelloAnswered ends the session's rehello commands with the hello that
// came (was: the hello the session had before), and forgets the ask.
func (s *Service) rehelloAnswered(ctx context.Context, session *models.MinionSession, was models.MinionHello, claim bool) {
	if session.RehelloAt != nil {
		if err := s.sessions.RehelloDone(ctx, session.SessionID); err != nil {
			s.logger.Warn("forget a rehello", zap.String("session_id", session.SessionID.String()), zap.Error(err))
		}
		session.RehelloAt, session.RehelloSends = nil, 0
	}
	out := helloSays(session.Hello, claim) + helloChanges(was, session.Hello)
	s.endRehelloCommands(ctx, session, true, out, "")
}

// rehelloUnanswered ends the session's rehello commands failed: the ask
// went with RehelloTries messages and no hello came. It is forgotten.
func (s *Service) rehelloUnanswered(ctx context.Context, session *models.MinionSession) {
	if err := s.sessions.RehelloDone(ctx, session.SessionID); err != nil {
		s.logger.Warn("forget a rehello", zap.String("session_id", session.SessionID.String()), zap.Error(err))
		return
	}
	session.RehelloAt, session.RehelloSends = nil, 0
	s.endRehelloCommands(ctx, session, false, "",
		fmt.Sprintf("no hello came: core asked with %d messages, and the minion did not answer", models.RehelloTries))
}

// endRehelloCommands ends the session's open rehello commands, ok or not.
// Errors are logged: nothing waits on them.
func (s *Service) endRehelloCommands(ctx context.Context, session *models.MinionSession, ok bool, out, why string) {
	if s.tasks == nil {
		return
	}
	open, err := s.tasks.OpenOfCommand(ctx, session.SessionID, models.CommandRehello)
	if err != nil {
		s.logger.Warn("rehello commands", zap.String("session_id", session.SessionID.String()), zap.Error(err))
		return
	}
	for _, t := range open {
		after, _, err := s.tasks.AddResult(ctx, session.SessionID, t.TaskID, repository.MinionTaskResult{
			Seq: 1, Done: true, OK: &ok, Out: []byte(out), Err: why, At: s.now(),
		})
		if err != nil {
			s.logger.Warn("end a rehello command", zap.String("task_id", t.TaskID.String()), zap.Error(err))
			continue
		}
		if after != nil {
			s.bus.Publish(eventbus.NewMinionTaskChangedEvent(eventbus.MinionTaskEventPayload{
				TaskID: after.TaskID.String(), SessionID: after.SessionID.String(), OperationID: after.OperationID.String(),
			}))
		}
	}
}

// helloSays is a hello as a rehello command's output's first line.
func helloSays(h models.MinionHello, claim bool) string {
	who := h.User
	if h.Domain != "" && who != "" {
		who = h.Domain + `\` + who
	}
	integrity := ""
	if h.Integrity != "" {
		integrity = "integrity " + h.Integrity
	}
	// The round trip the session has: this hello's, or the last one said
	// when it says none.
	roundtrip := ""
	if h.RoundtripTime != nil {
		roundtrip = fmt.Sprintf("round trip %d ms", *h.RoundtripTime)
	}
	parts := []string{}
	for _, p := range []string{h.Host, who, fmt.Sprintf("pid %d", h.PID), integrity, h.OS + "/" + h.Arch, h.Version, roundtrip} {
		if p != "" && p != "/" {
			parts = append(parts, p)
		}
	}
	lead := "hello"
	if claim {
		lead = "hello from a new process that claimed the session"
	}
	return lead + ": " + strings.Join(parts, " · ") + "\n"
}

// helloChanges says what a hello changed of the one before, a line each;
// "nothing changed" when nothing did.
func helloChanges(was, now models.MinionHello) string {
	var b strings.Builder
	field := func(name, a, z string) {
		if a != z {
			fmt.Fprintf(&b, "  %s: %s → %s\n", name, orNone(a), orNone(z))
		}
	}
	field("host", was.Host, now.Host)
	field("user", was.User, now.User)
	field("domain", was.Domain, now.Domain)
	field("integrity", was.Integrity, now.Integrity)
	field("pid", fmt.Sprint(was.PID), fmt.Sprint(now.PID))
	field("process", was.Proc, now.Proc)
	field("os", was.OS+"/"+was.Arch, now.OS+"/"+now.Arch)
	field("version", was.Version, now.Version)
	field("machine id", was.MachineID, now.MachineID)
	if !slices.Equal(was.Addrs, now.Addrs) {
		field("addresses", strings.Join(was.Addrs, ", "), strings.Join(now.Addrs, ", "))
	}
	if b.Len() == 0 {
		return "nothing changed\n"
	}
	return "changed:\n" + b.String()
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}
