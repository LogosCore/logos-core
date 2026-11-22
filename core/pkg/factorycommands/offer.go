package factorycommands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/logoscore/logos-core/core/pkg/credentialrefs"
	"github.com/logoscore/logos-core/core/pkg/messaging"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// underWayLimit bounds the tasks under way one tick looks at.
const underWayLimit = 1000

// Tick runs one pass: it ends the tasks core no longer waits for (their
// instance gone, or silent too long), then offers the tasks whose offer is
// due. The loop calls it; tests call it directly.
func (s *Service) Tick(ctx context.Context) {
	load := s.expire(ctx)
	for range s.cfg.Offers {
		now := s.now()
		t, err := s.tasks.ClaimFactoryOffer(ctx, s.id, now.Add(s.cfg.Lease), now)
		if err != nil {
			s.logger.Warn("factory commands: claim a task to offer", zap.Error(err))
			return
		}
		if t == nil {
			return
		}
		s.offer(ctx, t, load)
	}
}

// expire looks at the tasks under way: one whose instance is no longer
// registered fails, as does one its instance sent no report about for
// protocol.FactoryCommandNoReport (its command stopped there); one offered by
// a core that stopped before the answer goes back to wait. It returns how
// many tasks each instance runs: the next offers go to the one with the
// fewest first.
func (s *Service) expire(ctx context.Context) map[string]int {
	load := map[string]int{}
	under, err := s.tasks.FactoryUnderWay(ctx, underWayLimit)
	if err != nil {
		s.logger.Warn("factory commands: list the tasks under way", zap.Error(err))
		return load
	}
	if len(under) == 0 {
		return load
	}
	active, err := s.modules.ListActive(ctx)
	if err != nil {
		s.logger.Warn("factory commands: list registered modules", zap.Error(err))
		return load
	}
	live := map[string]bool{}
	for _, m := range active {
		live[m.Instance] = true
	}
	now := s.now()
	for i := range under {
		t := &under[i]
		f := t.Factory
		switch {
		case f.AcceptedAt == nil && f.ReportAt == nil && f.OfferedAt != nil && now.Sub(*f.OfferedAt) > 3*s.cfg.CallTimeout:
			// Offered, and no answer came (the core that offered it stopped
			// meanwhile): offered again.
			if _, err := s.tasks.UnofferFactory(ctx, t.TaskID, f.Instance); err != nil {
				s.logger.Warn("factory commands: offer a task again", zap.String("task_id", t.TaskID.String()), zap.Error(err))
			}
		case !live[f.Instance]:
			s.fail(ctx, t, fmt.Sprintf("factory instance %s stopped while it ran the task", f.Instance), false)
		case f.ReportAt != nil && now.Sub(*f.ReportAt) > protocol.FactoryCommandNoReport:
			s.fail(ctx, t, fmt.Sprintf("its factory sent no report for %s", protocol.FactoryCommandNoReport), true)
		default:
			load[f.Instance]++
		}
	}
	return load
}

// offer offers a claimed task to its factory's instances in turn, the one
// with the fewest tasks first: the first to accept runs it; a busy or
// unreachable one passes it on; any other answer is a refusal, and the task
// fails. When none takes it, it waits, and is offered again next tick.
//
// The task is marked offered to an instance before the offer goes there: an
// instance reports as soon as it accepts, which can be before its reply
// reaches core. One that does not take it puts the task back.
func (s *Service) offer(ctx context.Context, t *models.MinionTask, load map[string]int) {
	sess, b, gone, err := s.sessionBuild(ctx, t)
	switch {
	case gone != "":
		s.fail(ctx, t, gone, false)
		return
	case err != nil:
		s.wait(ctx, t, "core could not read the task's session: "+err.Error())
		return
	}
	insts, err := s.instances(ctx, t, load)
	if err != nil {
		s.wait(ctx, t, err.Error())
		return
	}
	if len(insts) == 0 {
		s.wait(ctx, t, fmt.Sprintf("no registered %q factory instance runs %s", t.Factory.Module, t.Command))
		return
	}
	req, err := s.request(ctx, t, sess, b)
	var unusable *credentialrefs.Unusable
	switch {
	case errors.As(err, &unusable):
		// Gone, or not of a type its param takes any more: no offer brings
		// it back.
		s.fail(ctx, t, unusable.Why, false)
		return
	case errors.Is(err, errTaskFileGone):
		s.fail(ctx, t, err.Error(), false)
		return
	case err != nil:
		s.wait(ctx, t, "core could not make the call: "+err.Error())
		return
	}
	if err := req.Validate(); err != nil {
		s.fail(ctx, t, "core could not make the call: "+err.Error(), false)
		return
	}
	why := ""
	for _, inst := range insts {
		ok, err := s.tasks.OfferFactory(ctx, t.TaskID, s.id, inst.Instance, s.now())
		if err != nil || !ok {
			return // stopped meanwhile
		}
		taken, refusal, reason := s.offerTo(ctx, t, inst, req)
		switch {
		case taken:
			return
		case refusal != "":
			s.fail(ctx, t, refusal, false)
			return
		}
		why = reason
		if back, err := s.tasks.UnofferFactory(ctx, t.TaskID, inst.Instance); err != nil || !back {
			// The instance reported meanwhile: it runs the task, its
			// acceptance lost on the way back.
			return
		}
	}
	s.wait(ctx, t, why)
}

// offerTo offers the task, marked offered to inst, there. taken when inst
// took it; a refusal says why the task fails; otherwise why says why inst
// passed it on.
func (s *Service) offerTo(ctx context.Context, t *models.MinionTask, inst models.Module, req protocol.FactoryCommandRequest) (taken bool,
	refusal, why string) {
	rep, err := s.rpc.Call(ctx, inst.RPCQueue, protocol.TypeFactoryCommand, req, s.cfg.CallTimeout)
	switch {
	case errors.Is(err, messaging.ErrRPCTimeout):
		return false, "", fmt.Sprintf("%s did not answer in time", inst.Instance)
	case err != nil:
		return false, "", fmt.Sprintf("%s: %v", inst.Instance, err)
	case rep.Status != protocol.StatusOK || rep.Error != nil:
		code, msg := protocol.CodeInternalError, "no reason given"
		if rep.Error != nil {
			code, msg = rep.Error.Code, rep.Error.Message
		}
		if code == protocol.CodeBusy {
			return false, "", fmt.Sprintf("%s is busy", inst.Instance)
		}
		return false, fmt.Sprintf("factory instance %s refused the task: %s: %s", inst.Instance, code, msg), ""
	}
	var reply protocol.FactoryCommandReply
	if json.Unmarshal(rep.Payload, &reply) != nil || reply.Validate() != nil || reply.TaskID != t.TaskID.String() {
		return false, "", fmt.Sprintf("%s answered with no acceptance", inst.Instance)
	}
	after, err := s.tasks.AcceptFactory(ctx, t.TaskID, inst.Instance, s.now())
	if err != nil {
		// Stopped meanwhile: its cancel went to the instance, or the
		// instance hears not_found at its first report.
		if !repository.IsNotFound(err) {
			s.logger.Warn("factory commands: record a task taken", zap.String("task_id", t.TaskID.String()), zap.Error(err))
		}
		return true, "", ""
	}
	s.logger.Info("factory command's task started", zap.String("task_id", t.TaskID.String()), zap.String("command", t.Command),
		zap.String("instance", inst.Instance))
	s.publish(after)
	return true, "", ""
}

// wait leaves a claimed task to wait for an instance, saying why: offered
// again next tick. The task changes, and is published, only when why is
// news.
func (s *Service) wait(ctx context.Context, t *models.MinionTask, why string) {
	note := t.Factory.Waiting != why || t.Factory.WaitingSince == nil
	after, err := s.tasks.WaitFactory(ctx, t.TaskID, s.id, s.now().Add(s.cfg.Tick), why, note, s.now())
	if err != nil {
		if !repository.IsNotFound(err) {
			s.logger.Warn("factory commands: leave a task to wait", zap.String("task_id", t.TaskID.String()), zap.Error(err))
		}
		return
	}
	if note {
		s.logger.Info("factory command's task waits for an instance", zap.String("task_id", t.TaskID.String()), zap.String("why", why))
		s.publish(after)
	}
}

// sessionBuild are a task's session and its build; gone says why the task
// can never run (its session or its build is gone), err a read that failed.
func (s *Service) sessionBuild(ctx context.Context, t *models.MinionTask) (*models.MinionSession, *models.Build, string, error) {
	sess, err := s.sessions.FindBySessionID(ctx, t.SessionID)
	if repository.IsNotFound(err) {
		return nil, nil, "its session is gone", nil
	}
	if err != nil {
		return nil, nil, "", err
	}
	b, err := s.builds.FindByBuildID(ctx, sess.BuildID)
	if repository.IsNotFound(err) {
		return nil, nil, "its session's build is gone: the factory would get no build", nil
	}
	if err != nil {
		return nil, nil, "", err
	}
	if b.Request == "" {
		return nil, nil, "its session's build has no request kept: the factory would get no build", nil
	}
	return sess, b, "", nil
}

// instances are the registered instances of the task's factory kind whose
// schema declares its command a factory command, whatever version its build
// was made with: those that run the fewest tasks (load) first.
func (s *Service) instances(ctx context.Context, t *models.MinionTask, load map[string]int) ([]models.Module, error) {
	mods, err := s.modules.ListActive(ctx)
	if err != nil {
		return nil, fmt.Errorf("core could not list the factory's instances: %w", err)
	}
	var out []models.Module
	for _, m := range mods {
		if m.Type != protocol.ServiceFactory || m.Name != t.Factory.Module || m.RPCQueue == "" || !m.Build.Current() {
			continue
		}
		if _, ok := m.Build.FactoryCommand(t.Command); ok {
			out = append(out, m)
		}
	}
	slices.SortStableFunc(out, func(a, b models.Module) int {
		if d := load[a.Instance] - load[b.Instance]; d != 0 {
			return d
		}
		return strings.Compare(a.Instance, b.Instance)
	})
	return out, nil
}
