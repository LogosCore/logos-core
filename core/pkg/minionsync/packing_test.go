package minionsync

import (
	"bytes"
	"compress/flate"
	"strings"
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// deflatedItem is a packed item holding data deflated, whatever it is.
func deflatedItem(t *testing.T, alg uint64, data []byte) protocol.PackedItem {
	t.Helper()
	var buf bytes.Buffer
	w, _ := flate.NewWriter(&buf, flate.BestSpeed)
	_, _ = w.Write(data)
	_ = w.Close()
	return protocol.PackedItem{T: protocol.ItemPacked, Alg: alg, Data: buf.Bytes()}
}

// packedOf is item, packed as a minion packs it.
func packedOf(t *testing.T, item any) protocol.PackedItem {
	t.Helper()
	raw, err := protocol.MarshalCBOR(item)
	if err != nil {
		t.Fatal(err)
	}
	return deflatedItem(t, protocol.PackDeflate, raw)
}

// unpacked are a reply's items as a minion reads them, and how many came
// packed.
func unpacked(t *testing.T, msg protocol.CoreMessage) ([]cbor.RawMessage, int) {
	t.Helper()
	n := 0
	out := make([]cbor.RawMessage, len(msg.Items))
	for i, it := range msg.Items {
		out[i] = it
		if protocol.ItemType(it) == protocol.ItemPacked {
			inner, err := protocol.Unpack(it, protocol.MaxMinionMessageSize)
			if err != nil {
				t.Fatalf("item %d does not unpack: %v", i, err)
			}
			out[i] = inner
			n++
		}
	}
	return out, n
}

// tasksOf are the task items among items.
func tasksOf(t *testing.T, items []cbor.RawMessage) []protocol.TaskItem {
	t.Helper()
	var out []protocol.TaskItem
	for _, raw := range items {
		if protocol.ItemType(raw) != protocol.ItemTask {
			continue
		}
		var ti protocol.TaskItem
		if err := protocol.UnmarshalCBOR(raw, &ti); err != nil {
			t.Fatal(err)
		}
		out = append(out, ti)
	}
	return out
}

// Where the build packs, the outbox packs each task as it adds it: five
// that fill a 2048-byte connection one by one go together; one whose args
// carry a credential goes as it is. Without compression nothing is packed.
func TestTasksGoPackedWhereTheBuildPacks(t *testing.T) {
	for _, on := range []bool{true, false} {
		r := newRig(t)
		sid, tasks := openSession(t, r)
		row := r.sessionRow(t, sid)
		row.ConnMaxMessage = 2048
		if on {
			row.Compression = protocol.CompressionDeflate
		}
		for range 5 {
			id := uuid.New()
			wire, _ := protocol.MarshalCBOR(protocol.Args{1: strings.Repeat("ls -la /usr/lib; ", 90)})
			tasks.rows = append(tasks.rows, models.MinionTask{TaskID: id, SessionID: sid, OperationID: r.build.OperationID,
				Command: "shell", Op: 11, WireArgs: wire, State: models.MinionTaskQueued})
		}
		secret := uuid.New()
		wire, _ := protocol.MarshalCBOR(protocol.Args{1: strings.Repeat("hunter2 ", 60)})
		tasks.rows = append(tasks.rows, models.MinionTask{TaskID: secret, SessionID: sid, OperationID: r.build.OperationID,
			Command: "login", Op: 12, WireArgs: wire, CredentialIDs: []uuid.UUID{uuid.New()}, State: models.MinionTaskQueued})

		reply, err := r.sendUnder(t, sid, r.keys, protocol.NewMessageID(), protocol.GetTasksItem{T: protocol.ItemGetTasks})
		if err != nil {
			t.Fatal(err)
		}
		if len(reply) > 2048+protocol.MinFrameSize {
			t.Fatalf("on %v: a reply of %d bytes over a connection of 2048", on, len(reply))
		}
		msg := r.openUnder(t, sid, r.keys, reply)
		items, packedN := unpacked(t, msg)
		got := tasksOf(t, items)
		if !on {
			if packedN != 0 || len(got) != 1 {
				t.Fatalf("off: %d packed, %d tasks", packedN, len(got))
			}
			continue
		}
		if len(got) != 6 || packedN != 5 {
			t.Fatalf("on: %d tasks, %d packed", len(got), packedN)
		}
		for _, raw := range msg.Items {
			if protocol.ItemType(raw) != protocol.ItemTask {
				continue
			}
			var ti protocol.TaskItem
			_ = protocol.UnmarshalCBOR(raw, &ti)
			if uuid.UUID(ti.Task) != secret {
				t.Fatalf("task %x went unpacked", ti.Task)
			}
		}
	}
}

// What a minion sends packed is taken in its place; one that does not
// unpack is answered by its index: bad_message, or too_large past the room
// its message has left.
func TestPackedItemsFromTheMinion(t *testing.T) {
	r := newRig(t)
	sid, tasks := openSession(t, r)
	id := uuid.New()
	tasks.rows = append(tasks.rows, models.MinionTask{TaskID: id, SessionID: sid, OperationID: r.build.OperationID,
		Command: "shell", Op: 11, State: models.MinionTaskQueued})
	reply, err := r.sendUnder(t, sid, r.keys, protocol.NewMessageID(),
		packedOf(t, protocol.GetTasksItem{T: protocol.ItemGetTasks}),
		deflatedItem(t, 2, []byte{0xa1, 0x00, 0x07}),
		deflatedItem(t, protocol.PackDeflate, make([]byte, protocol.MaxMinionMessageSize)),
		packedOf(t, packedOf(t, protocol.GetTasksItem{T: protocol.ItemGetTasks})))
	if err != nil {
		t.Fatal(err)
	}
	msg := r.openUnder(t, sid, r.keys, reply)
	if got := tasksOf(t, msg.Items); len(got) != 1 || uuid.UUID(got[0].Task) != id {
		t.Fatalf("the packed get_tasks brought %v", got)
	}
	want := map[int]int{1: protocol.ErrItemBadMessage, 2: protocol.ErrItemTooLarge, 3: protocol.ErrItemBadMessage}
	for _, raw := range itemsOfCore(msg, protocol.ItemErr) {
		var e protocol.ErrItem
		if err := protocol.UnmarshalCBOR(raw, &e); err != nil || e.Item == nil {
			t.Fatalf("an err %v", err)
		}
		if want[*e.Item] != e.Code {
			t.Fatalf("item %d: %s (%s)", *e.Item, protocol.ErrCodeName(e.Code), e.Msg)
		}
		delete(want, *e.Item)
	}
	if len(want) != 0 {
		t.Fatalf("not answered: %v", want)
	}
}

// Core seals packed what pays: a large answer goes packed, a small one,
// and every item of a session whose build does not pack, as they are.
func TestPackMessage(t *testing.T) {
	big, _ := protocol.MarshalCBOR(protocol.ErrItem{T: protocol.ItemErr, Code: protocol.ErrItemBadMessage, Msg: strings.Repeat("no such file ", 80)})
	small, _ := protocol.MarshalCBOR(protocol.ErrItem{T: protocol.ItemErr, Code: protocol.ErrItemBadMessage, Msg: "no"})
	delegate, _ := protocol.MarshalCBOR(protocol.DelegateItem{T: protocol.ItemDelegate, Task: make([]byte, 16), Step: protocol.DelegateOpen, N: 1,
		Values: map[string]any{"host": strings.Repeat("10.0.0.1 ", 80)}})
	items := []cbor.RawMessage{big, small, delegate}
	on := &models.MinionSession{Compression: protocol.CompressionDeflate}
	out := packMessage(on, items)
	if protocol.ItemType(out[0]) != protocol.ItemPacked || !bytes.Equal(out[1], small) || !bytes.Equal(out[2], delegate) {
		t.Fatalf("packed %d %d %d", protocol.ItemType(out[0]), protocol.ItemType(out[1]), protocol.ItemType(out[2]))
	}
	if !bytes.Equal(items[0], big) {
		t.Fatal("the caller's items changed")
	}
	for _, s := range []*models.MinionSession{nil, {}} {
		if out := packMessage(s, items); protocol.ItemType(out[0]) != protocol.ItemErr {
			t.Fatal("packed for a build that does not pack")
		}
	}
}
