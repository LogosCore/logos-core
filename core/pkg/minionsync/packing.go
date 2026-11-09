package minionsync

import (
	"errors"

	"github.com/fxamacker/cbor/v2"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// Compression (docs: contracts/minion-messages/wire-format/, Compression).
// Core packs the items it sends a minion whose build packs
// (models.MinionSession.Compression) where that pays (protocol.Pack), each
// in a context of its own: as the outbox adds them, so a connection that
// carries little takes more, and the rest (answers, chunks) as the message
// is sealed. It takes what any minion sends packed in its place
// (protocol.UnpackItems), whatever its build.

// packable reports whether core may pack item as it seals a message: never
// a task (its args may carry a credential: the outbox packs a task itself,
// packTask), nor a delegate's item (a call or an open carries a target's
// values and credentials; its frames are sealed, and do not shrink).
func packable(item cbor.RawMessage) bool {
	switch protocol.ItemType(item) {
	case protocol.ItemTask, protocol.ItemDelegate, protocol.ItemPacked:
		return false
	}
	return true
}

// pack is item as core sends it to session's minion: packed when its
// build packs, the item may go packed, and packing pays.
func pack(session *models.MinionSession, item cbor.RawMessage) cbor.RawMessage {
	if session == nil || session.Compression == "" || !packable(item) {
		return item
	}
	return packed(session, item)
}

// packTask is task t's item as core sends it to session's minion: packed
// as pack would, unless its args carry a credential.
func packTask(session *models.MinionSession, t *models.MinionTask, item cbor.RawMessage) cbor.RawMessage {
	if session == nil || session.Compression == "" || len(t.CredentialIDs) > 0 {
		return item
	}
	return packed(session, item)
}

// packed is item packed when that pays, as session's envelope writes it
// (protocol.PackIn), else item. Under a factory's envelope core packs
// nothing: the factory packs in its own format (docs: Codecs, Packing goes
// with the envelope).
func packed(session *models.MinionSession, item cbor.RawMessage) cbor.RawMessage {
	env, ok := sessionEnvelope(session)
	if !ok {
		return item
	}
	if out, _, err := protocol.PackIn(env, item); err == nil {
		return out
	}
	return item
}

// packMessage is items as core seals them for session's minion: each that
// may go packed, packed when that pays. A copy: what the caller keeps of
// what it sent (a call's items, to read its answer) stays as it was.
func packMessage(session *models.MinionSession, items []cbor.RawMessage) []cbor.RawMessage {
	if session == nil || session.Compression == "" || len(items) == 0 {
		return items
	}
	out := make([]cbor.RawMessage, len(items))
	for i, it := range items {
		out[i] = pack(session, it)
	}
	return out
}

// packedErr answers item i of items, a packed item the minion sent that
// did not unpack in env (protocol.UnpackItemsIn left it): too_large past
// the room its message had left, bad_message otherwise.
func packedErr(env protocol.MessageEnvelope, items []cbor.RawMessage, i int) ([]cbor.RawMessage, error) {
	_, err := protocol.UnpackIn(env, items[i], protocol.UnpackRoomIn(env, items, i))
	if errors.Is(err, protocol.ErrPackedTooLarge) {
		return errItem(protocol.ErrItemTooLarge, err.Error(), i)
	}
	msg := protocol.ErrPackedBad.Error()
	if err != nil {
		msg = err.Error()
	}
	return errItem(protocol.ErrItemBadMessage, msg, i)
}
