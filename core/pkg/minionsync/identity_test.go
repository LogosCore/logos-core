package minionsync

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// fakeIdentities keeps a session's history.
type fakeIdentities struct {
	repository.IMinionIdentityRepository
	rows []models.MinionIdentityEvent
}

func (f *fakeIdentities) Add(_ context.Context, e *models.MinionIdentityEvent) error {
	f.rows = append(f.rows, *e)
	return nil
}

func (f *fakeIdentities) PutTask(_ context.Context, e *models.MinionIdentityEvent) error {
	for i, r := range f.rows {
		if r.Scope == models.IdentityScopeTask && r.TaskID != nil && *r.TaskID == *e.TaskID {
			f.rows[i] = *e
			return nil
		}
	}
	f.rows = append(f.rows, *e)
	return nil
}

func (f *fakeIdentities) DropTask(_ context.Context, _, task uuid.UUID) error {
	kept := f.rows[:0]
	for _, r := range f.rows {
		if r.Scope != models.IdentityScopeTask || r.TaskID == nil || *r.TaskID != task {
			kept = append(kept, r)
		}
	}
	f.rows = kept
	return nil
}

func (f *fakeSessions) SetActing(_ context.Context, id uuid.UUID, acting *models.MinionIdentity) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.rows {
		if f.rows[i].SessionID == id {
			f.rows[i].Acting = acting
		}
	}
	return nil
}

func (f *fakeIdentities) scopes() []string {
	var out []string
	for _, r := range f.rows {
		out = append(out, r.Scope+":"+r.User+":"+r.How)
	}
	return out
}

// A session's identity: kept, the tasks acked after it stamped with it
// (not those before, nor one that said its own), each change in the
// history once; a credential named marked valid only when its task gave
// it; back to the process's own; a task of another session refused.
func TestIdentity(t *testing.T) {
	r := newRig(t)
	identities := &fakeIdentities{}
	r.svc.WithIdentities(identities)
	creds := &fakeCredentials{}
	r.svc.WithCredentials(creds)
	sid, row := helloWith(t, r, nil)
	cred := uuid.New()
	become, before, after, file := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	tasks := &fakeTasks{rows: []models.MinionTask{
		{TaskID: become, SessionID: sid, CredentialIDs: []uuid.UUID{cred}},
		{TaskID: before, SessionID: sid}, {TaskID: after, SessionID: sid}, {TaskID: file, SessionID: sid},
	}}
	r.svc.WithTasks(tasks)
	checkIn := checkInOf(t, r, sid)
	ack := func(ids ...uuid.UUID) protocol.AckItem {
		a := protocol.AckItem{T: protocol.ItemAck}
		for _, id := range ids {
			a.Tasks = append(a.Tasks, id[:])
		}
		return a
	}
	root := protocol.IdentityItem{T: protocol.ItemIdentity, Scope: protocol.IdentitySession, Task: become[:], User: "root",
		Integrity: protocol.IntegrityRoot, How: protocol.IdentityBecome, Credential: cred.String()}
	own := protocol.IdentityItem{T: protocol.ItemIdentity, Scope: protocol.IdentityTask, Task: file[:]}
	errsOf := func(items []any) []protocol.ErrItem {
		var out []protocol.ErrItem
		for _, it := range checkIn(items...) {
			if protocol.ItemType(it) == protocol.ItemErr {
				var e protocol.ErrItem
				_ = protocol.UnmarshalCBOR(it, &e)
				out = append(out, e)
			}
		}
		return out
	}
	if errs := errsOf([]any{ack(before), root, own, ack(after, file)}); len(errs) != 0 {
		t.Fatalf("errs %+v", errs)
	}
	if row.Acting == nil || row.Acting.User != "root" || row.Acting.How != "become" || row.Acting.Integrity != "root" ||
		row.Acting.TaskID == nil || *row.Acting.TaskID != become || row.Acting.CredentialID == nil {
		t.Fatalf("acting %+v", row.Acting)
	}
	if a := tasks.find(after).As; a == nil || a.User != "root" {
		t.Fatalf("the task after: %+v", a)
	}
	if a := tasks.find(before).As; a != nil {
		t.Fatalf("the task before: %+v", a)
	}
	if a := tasks.find(file).As; a == nil || !a.Own() {
		t.Fatalf("the file task: %+v", a)
	}
	if len(creds.worked) != 1 || creds.worked[0] != cred {
		t.Fatalf("worked %v", creds.worked)
	}
	// Sent again: nothing new.
	checkIn(root)
	want := []string{"process:" + row.Hello.User + ":", "session:root:become"}
	if got := identities.scopes(); len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("history %v", got)
	}
	// A credential its task did not give; a task not the session's.
	stranger := root
	stranger.Credential = uuid.NewString()
	other := own
	o := uuid.New()
	other.Task = o[:]
	errs := errsOf([]any{stranger, other})
	if len(errs) != 2 || errs[0].Code != protocol.ErrItemBadMessage || errs[1].Code != protocol.ErrItemUnknownTask || len(errs[1].Task) != 16 {
		t.Fatalf("errs %+v", errs)
	}
	// Back to the process's own: who that is, in the history.
	checkIn(protocol.IdentityItem{T: protocol.ItemIdentity, Scope: protocol.IdentitySession, Task: become[:]})
	if row.Acting != nil {
		t.Fatalf("acting %+v", row.Acting)
	}
	if last := identities.rows[len(identities.rows)-1]; last.Scope != models.IdentityScopeSession || last.How != "" || last.User != row.Hello.User {
		t.Fatalf("the revert: %+v", last)
	}
}

// A call or an open to an agentless kind carries the task whose identity is
// in effect, its args as it went.
func TestActingOf(t *testing.T) {
	r := newRig(t)
	sid, row := helloWith(t, r, nil)
	become := uuid.New()
	args, _ := protocol.MarshalCBOR(protocol.Args{1: "postgres"})
	r.svc.WithTasks(&fakeTasks{rows: []models.MinionTask{{TaskID: become, SessionID: sid, Op: 10, WireArgs: args}}})
	if a := r.svc.actingOf(context.Background(), row); a != nil {
		t.Fatalf("acting %+v without an identity", a)
	}
	row.Acting = &models.MinionIdentity{User: "postgres", How: "become", TaskID: &become}
	a := r.svc.actingOf(context.Background(), row)
	if a == nil || a.Cmd != 10 || a.Args[1] != "postgres" || uuid.UUID(a.Task) != become || a.Validate() != nil {
		t.Fatalf("acting %+v", a)
	}
}
