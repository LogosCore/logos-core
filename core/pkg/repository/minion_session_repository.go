package repository

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/database"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
	opts "github.com/qiniu/qmgo/options"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	minionSessionCollection = "minion_sessions"
	// minionSessionCounterCollection holds one counter per operation: the
	// last session number it gave.
	minionSessionCounterCollection = "minion_session_counters"
)

// IMinionSessionRepository keeps minion sessions (models.MinionSession).
type IMinionSessionRepository interface {
	// NextNumber reserves the operation's next session number: 1, 2, 3, …
	NextNumber(ctx context.Context, operationID uuid.UUID) (int64, error)
	Insert(ctx context.Context, s *models.MinionSession) error
	// ByMachine are the operation's sessions whose hello says machineID,
	// newest first (the file browser's machine; docs:
	// contracts/file-browser.md).
	ByMachine(ctx context.Context, operationID uuid.UUID, machineID string) ([]models.MinionSession, error)
	// FindBySessionID returns one session; IsNotFound on a miss.
	FindBySessionID(ctx context.Context, sessionID uuid.UUID) (*models.MinionSession, error)
	// CheckIn records a message under the session id (MinionSessionCheckIn):
	// the time, the end of the wait for the first check-in, where it came
	// from in the session's external addresses, and when the session turns
	// late.
	CheckIn(ctx context.Context, sessionID uuid.UUID, c MinionSessionCheckIn) error
	// SetHello replaces what the session's hello said (a rehello), with the
	// address the session shows now.
	SetHello(ctx context.Context, sessionID uuid.UUID, hello models.MinionHello, address string, picked bool) error
	// SetActing keeps who the session's tasks run as; nil for its
	// process's own.
	SetActing(ctx context.Context, sessionID uuid.UUID, acting *models.MinionIdentity) error
	// SetConn records the connection the minion uses (its place in the
	// build's connections, kind and name), and with change, the move it
	// reported, first in the session's ConnChanges (capped at
	// MaxSessionConnChanges).
	SetConn(ctx context.Context, sessionID uuid.UUID, conn models.MinionSessionConn, change *models.MinionSessionConnChange) error
	// SetAddress sets the address the session shows, one of its hello's;
	// IsNotFound when the session has no such address.
	SetAddress(ctx context.Context, sessionID uuid.UUID, address string, picked bool) error
	// FindByOperationIDWithCursor is a page of an operation's sessions as q
	// asks, from after (nil: the start); forward false pages back.
	FindByOperationIDWithCursor(ctx context.Context, operationID uuid.UUID, q MinionSessionQuery, after *MinionSessionKeyset, limit int64, forward bool) ([]models.MinionSession, error)
	CountByOperationID(ctx context.Context, operationID uuid.UUID, q MinionSessionQuery) (int64, error)
	// Values are the distinct values an operation's sessions have in a
	// column, sorted; missing values are left out.
	Values(ctx context.Context, operationID uuid.UUID, column string) ([]string, error)
	// SetArchived archives a session or restores it; IsNotFound when there
	// is none.
	SetArchived(ctx context.Context, sessionID uuid.UUID, archived bool) error
	// SetArchivedMany archives or restores the operation's sessions with
	// these ids, and SetArchivedMatching those q lists (its Archived view,
	// search and filters; not its sort): one write each. Both return how
	// many changed.
	SetArchivedMany(ctx context.Context, operationID uuid.UUID, ids []uuid.UUID, archived bool) (int64, error)
	SetArchivedMatching(ctx context.Context, operationID uuid.UUID, q MinionSessionQuery, archived bool) (int64, error)
	// SetFolderMany and SetFolderMatching put sessions in a folder (nil:
	// none); ClearFolder takes every session out of one (it is gone).
	SetFolderMany(ctx context.Context, operationID uuid.UUID, ids []uuid.UUID, folder *uuid.UUID) (int64, error)
	SetFolderMatching(ctx context.Context, operationID uuid.UUID, q MinionSessionQuery, folder *uuid.UUID) (int64, error)
	ClearFolder(ctx context.Context, operationID, folder uuid.UUID) (int64, error)
	// FolderCounts are the operation's open sessions not archived: all of
	// them, and by folder.
	FolderCounts(ctx context.Context, operationID uuid.UUID) (int64, map[uuid.UUID]int64, error)
	// SessionIDs are the ids of the sessions q lists (not its sort), at most
	// limit: what a bulk change names by matching.
	SessionIDs(ctx context.Context, operationID uuid.UUID, q MinionSessionQuery, limit int64) ([]uuid.UUID, error)
	// OwnSessionIDs keeps the ids that are the operation's sessions.
	OwnSessionIDs(ctx context.Context, operationID uuid.UUID, ids []uuid.UUID) ([]uuid.UUID, error)
	// BackfillProcessNames turns the process paths stored before core kept
	// a hello's proc as a name into names (models.ProcessName).
	BackfillProcessNames(ctx context.Context) (int64, error)
	// SetDescription replaces a session's description; IsNotFound when
	// there is none.
	SetDescription(ctx context.Context, sessionID uuid.UUID, description string) error
	// BackfillDescriptions gives sessions from before they had a
	// description their build's.
	BackfillDescriptions(ctx context.Context) (int64, error)
	// SetPersisted marks a session persisted: a build names it.
	SetPersisted(ctx context.Context, sessionID uuid.UUID) error
	// AddFeedback counts feedback its minion sent at at: errors and warnings
	// unread (models.MinionSession.FeedbackErrors), and when the latest came.
	AddFeedback(ctx context.Context, sessionID uuid.UUID, errors, warnings int, at time.Time) error
	// MarkFeedbackRead marks the session's feedback read at at, for
	// everyone: its unread counts go back to 0.
	MarkFeedbackRead(ctx context.Context, sessionID uuid.UUID, at time.Time) error
	// SetMaxTasks records how many tasks the minion runs at once, as it
	// said (models.MinionSession.MaxTasks); nil: it says none. The count
	// of the operator's limit sent unanswered starts again.
	SetMaxTasks(ctx context.Context, sessionID uuid.UUID, max *int) error
	// SetMaxTasksWanted records the operator's limit, which core sends the
	// minion until it says it has it (models.MinionSession.MaxTasksSet),
	// its count of sends at 0.
	SetMaxTasksWanted(ctx context.Context, sessionID uuid.UUID, max int) error
	// MaxTasksSent counts one more message that carried the operator's
	// limit (models.MinionSession.MaxTasksSends).
	MaxTasksSent(ctx context.Context, sessionID uuid.UUID) error
	// SetConnected records whether a persistent connection serving the
	// session is open (models.MinionSession.Connected).
	SetConnected(ctx context.Context, sessionID uuid.UUID, connected bool) error
	// ClearConnected says no session is connected: core held the
	// connections, and they went with the process before.
	ClearConnected(ctx context.Context) (int64, error)
	// Claim gives an open session to the process that claimed it: its
	// keys, build and hello, the previous keys kept as replaced, the restart
	// recorded. IsNotFound when the session is not open.
	Claim(ctx context.Context, sessionID uuid.UUID, c MinionSessionClaim) error
	// BackfillLostAt gives sessions with a schedule and late_at but no
	// lost_at (from before core kept one) theirs, from those two.
	BackfillLostAt(ctx context.Context) (int64, error)
	// CloseByOperationID closes an operation's sessions (a tombstone each)
	// and forgets its session numbers.
	CloseByOperationID(ctx context.Context, operationID uuid.UUID, reason string, at time.Time) error
	// Close closes one session (a tombstone); nothing when it is closed
	// already or gone.
	Close(ctx context.Context, sessionID uuid.UUID, reason string, at time.Time) error
}

type minionSessionRepository struct{ coll, counters database.Collection }

func NewMinionSessionRepository(db database.Database) IMinionSessionRepository {
	db.EnsureIndexes(context.Background(), minionSessionCollection, []opts.IndexModel{
		{Key: []string{"session_id"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
		{Key: []string{"operation_id", "number"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
		{Key: []string{"operation_id", "-created_at", "-_id"}},
		{Key: []string{"operation_id", "hello.machine_id"}},
		{Key: []string{"operation_id", "late_at"}},
		{Key: []string{"operation_id", "lost_at"}},
		{Key: []string{"operation_id", "folder_id"}},
		// A session without purge_at (checked in, open) stays.
		{Key: []string{"purge_at"}, IndexOptions: new(options.IndexOptions).SetExpireAfterSeconds(0)},
	})
	db.EnsureIndexes(context.Background(), minionSessionCounterCollection, []opts.IndexModel{
		{Key: []string{"operation_id"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
	})
	return &minionSessionRepository{
		coll: db.Collection(minionSessionCollection), counters: db.Collection(minionSessionCounterCollection),
	}
}

func (r *minionSessionRepository) NextNumber(ctx context.Context, operationID uuid.UUID) (int64, error) {
	raw, err := r.counters.RawCollection()
	if err != nil {
		return 0, err
	}
	var counter struct {
		Seq int64 `bson:"seq"`
	}
	err = raw.FindOneAndUpdate(ctx,
		bson.M{"operation_id": operationID},
		bson.M{"$inc": bson.M{"seq": 1}},
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
	).Decode(&counter)
	return counter.Seq, err
}

// withRev is a session's update (operators, not a pipeline) with its rev
// counted (models.MinionSession.Rev); revStage is the same in a pipeline's
// $set.
func withRev(update bson.M) bson.M {
	out := make(bson.M, len(update)+1)
	for k, v := range update {
		out[k] = v
	}
	inc := bson.M{"rev": 1}
	if have, ok := update["$inc"].(bson.M); ok {
		for k, v := range have {
			inc[k] = v
		}
	}
	out["$inc"] = inc
	return out
}

var revStage = bson.M{"$add": bson.A{bson.M{"$ifNull": bson.A{"$rev", 0}}, 1}}

func (r *minionSessionRepository) Insert(ctx context.Context, s *models.MinionSession) error {
	s.Rev = 1
	_, err := r.coll.InsertOne(ctx, s)
	return err
}

func (r *minionSessionRepository) FindBySessionID(ctx context.Context, sessionID uuid.UUID) (*models.MinionSession, error) {
	var s models.MinionSession
	if err := r.coll.Find(ctx, bson.M{"session_id": sessionID}).One(&s); err != nil {
		return nil, err
	}
	return &s, nil
}

// MinionSessionCheckIn is what a check-in records.
type MinionSessionCheckIn struct {
	At time.Time
	// Peer is the IP address it came from; "" for unknown.
	Peer string
	// Sleep is a schedule the message said (a sleep_update, a rehello); nil
	// leaves the session's. LateAt and LostAt are when the session turns
	// late and lost from now; nil leaves them (no schedule known).
	Sleep          *models.MinionSessionSleep
	LateAt, LostAt *time.Time
	// Untimed: the minion's connection says whether it is there, not a
	// schedule (models.LateBySchedule): late_at and lost_at go.
	Untimed bool
	// Path: the check-in is a message's, which says how it came: through
	// the delegates Via (nearest first) since ViaSince, or none (straight
	// from a channel). Without it (a connection's keepalive) the session's
	// path stays.
	Path     bool
	Via      []models.MinionSessionVia
	ViaSince time.Time
}

func (r *minionSessionRepository) CheckIn(ctx context.Context, sessionID uuid.UUID, c MinionSessionCheckIn) error {
	set := bson.M{"last_seen_at": c.At}
	if c.Sleep != nil {
		set["sleep"] = c.Sleep
	}
	if c.LateAt != nil {
		set["late_at"] = *c.LateAt
	}
	if c.LostAt != nil {
		set["lost_at"] = *c.LostAt
	}
	// A check-in also restores an archived session: its minion is back.
	unset := []string{"purge_at", "archived"}
	if c.Untimed {
		delete(set, "late_at")
		delete(set, "lost_at")
		unset = append(unset, "late_at", "lost_at")
	}
	switch {
	case c.Path && len(c.Via) > 0:
		set["via"], set["via_since"] = c.Via, c.ViaSince
	case c.Path:
		unset = append(unset, "via", "via_since")
	}
	if c.Peer == "" {
		u := bson.M{}
		for _, k := range unset {
			u[k] = ""
		}
		return r.coll.UpdateOne(ctx, bson.M{"session_id": sessionID}, withRev(bson.M{"$set": set, "$unset": u}))
	}
	return r.coll.UpdateOne(ctx, bson.M{"session_id": sessionID}, checkInPipeline(c.At, c.Peer, set, unset))
}

// checkInPipeline is CheckIn's update with a peer address, in one write,
// with the check-in's other fields (set):
//
//	old   = the peers entry for peer, if any
//	peers = slice(sort(peers without it + {peer, old.first_seen ?? at, at,
//	        old.count+1}, by last_seen desc), MaxSessionPeers)
//
// The address goes in through $literal, as a client's value must.
func checkInPipeline(at time.Time, peer string, set bson.M, unset []string) bson.A {
	lit := bson.M{"$literal": peer}
	byAddress := func(op string) bson.M {
		return bson.M{"$filter": bson.M{"input": "$$cur", "cond": bson.M{op: bson.A{"$$this.address", lit}}}}
	}
	stage := bson.M{
		"peer_address": lit,
		"peers": bson.M{"$let": bson.M{
			"vars": bson.M{"cur": bson.M{"$ifNull": bson.A{"$peers", bson.A{}}}},
			"in": bson.M{"$let": bson.M{
				"vars": bson.M{"old": bson.M{"$first": byAddress("$eq")}, "rest": byAddress("$ne")},
				"in": bson.M{"$slice": bson.A{
					bson.M{"$sortArray": bson.M{
						"input": bson.M{"$concatArrays": bson.A{"$$rest", bson.A{bson.M{
							"address":    lit,
							"first_seen": bson.M{"$ifNull": bson.A{"$$old.first_seen", at}},
							"last_seen":  at,
							"count":      bson.M{"$add": bson.A{bson.M{"$ifNull": bson.A{"$$old.count", 0}}, 1}},
						}}}},
						"sortBy": bson.M{"last_seen": -1},
					}},
					models.MaxSessionPeers,
				}},
			}},
		}},
	}
	// The check-in's own fields: values, not field paths.
	for k, v := range set {
		stage[k] = bson.M{"$literal": v}
	}
	stage["rev"] = revStage
	u := bson.A{}
	for _, k := range unset {
		u = append(u, k)
	}
	return bson.A{bson.M{"$set": stage}, bson.M{"$unset": u}}
}

func (r *minionSessionRepository) SetHello(ctx context.Context, sessionID uuid.UUID, hello models.MinionHello, address string, picked bool) error {
	return r.coll.UpdateOne(ctx, bson.M{"session_id": sessionID},
		withRev(bson.M{"$set": bson.M{"hello": hello, "address": address, "address_picked": picked}}))
}

func (r *minionSessionRepository) SetActing(ctx context.Context, sessionID uuid.UUID, acting *models.MinionIdentity) error {
	update := bson.M{"$unset": bson.M{"acting": ""}}
	if acting != nil {
		update = bson.M{"$set": bson.M{"acting": acting}}
	}
	return r.coll.UpdateOne(ctx, bson.M{"session_id": sessionID}, withRev(update))
}

func (r *minionSessionRepository) SetConn(ctx context.Context, sessionID uuid.UUID, conn models.MinionSessionConn, change *models.MinionSessionConnChange) error {
	update := bson.M{"$set": bson.M{"conn": conn.Place, "conn_kind": conn.Kind, "conn_name": conn.Name,
		"conn_max_message": conn.MaxMessage}}
	if !models.LateBySchedule(conn.Kind) {
		// On a connection that says whether the minion is there: no
		// longer late by a schedule.
		update["$unset"] = bson.M{"late_at": "", "lost_at": ""}
	}
	if change != nil {
		update["$push"] = bson.M{"conn_changes": bson.M{
			"$each": bson.A{change}, "$position": 0, "$slice": models.MaxSessionConnChanges,
		}}
	}
	return r.coll.UpdateOne(ctx, bson.M{"session_id": sessionID}, withRev(update))
}

func (r *minionSessionRepository) SetAddress(ctx context.Context, sessionID uuid.UUID, address string, picked bool) error {
	filter := bson.M{"session_id": sessionID}
	if address != "" {
		filter["hello.addrs"] = address
	}
	return r.coll.UpdateOne(ctx, filter, withRev(bson.M{"$set": bson.M{"address": address, "address_picked": picked}}))
}

// minionSessionListFilter is an operation's sessions, searched (searchPattern)
// over what the Sessions page shows: the host and process, the build and its
// factory, and the ids. A number ("12", "#12") also matches the session
// numbered so.
func minionSessionListFilter(operationID uuid.UUID, search string) bson.M {
	q := bson.M{"operation_id": operationID}
	if search == "" {
		return q
	}
	rx := bson.M{"$regex": searchPattern(search), "$options": "i"}
	or := bson.A{}
	for _, field := range []string{
		"hello.host", "hello.user", "hello.domain", "hello.proc", "hello.os", "hello.arch", "hello.integrity", "hello.machine_id",
		"hello.addrs", "hello.version", "description", "build_description", "module_name", "target", "platform", "peers.address",
	} {
		or = append(or, bson.M{field: rx})
	}
	if n, ok := ParseSessionSearchNumber(search); ok {
		or = append(or, bson.M{"number": n})
	}
	// Ids are binary UUIDs: matched whole, as typed.
	if id, err := uuid.Parse(search); err == nil {
		or = append(or, bson.M{"session_id": id}, bson.M{"build_id": id})
	}
	q["$or"] = or
	return q
}

func (r *minionSessionRepository) FindByOperationIDWithCursor(ctx context.Context, operationID uuid.UUID, q MinionSessionQuery, after *MinionSessionKeyset, limit int64, forward bool) ([]models.MinionSession, error) {
	filter := minionSessionFilter(operationID, q, time.Now().UTC())
	if after != nil {
		ascending := !q.Sort.Desc == forward
		filter = bson.M{"$and": bson.A{filter, keysetFilter(models.MinionSessionColumns[q.Sort.Column].SortBy(), after, ascending)}}
	}
	out := []models.MinionSession{}
	err := r.coll.Find(ctx, filter).Sort(sortFields(q.Sort, forward)...).Limit(limit).All(&out)
	if !forward {
		slices.Reverse(out)
	}
	return out, err
}

func (r *minionSessionRepository) CountByOperationID(ctx context.Context, operationID uuid.UUID, q MinionSessionQuery) (int64, error) {
	return r.coll.Count(ctx, minionSessionFilter(operationID, q, time.Now().UTC()))
}

func (r *minionSessionRepository) Values(ctx context.Context, operationID uuid.UUID, column string) ([]string, error) {
	col, ok := models.MinionSessionColumns[column]
	if !ok || (col.Kind != models.SessionColumnEnum && col.Kind != models.SessionColumnBool) {
		return nil, fmt.Errorf("%s has no list of values", column)
	}
	raw, err := r.coll.RawCollection()
	if err != nil {
		return nil, err
	}
	found, err := raw.Distinct(ctx, col.Field, bson.M{"$and": bson.A{bson.M{"operation_id": operationID}, openSessions(time.Now().UTC())}})
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, v := range found {
		switch v := v.(type) {
		case string:
			if v != "" {
				out = append(out, v)
			}
		case bool:
			out = append(out, strconv.FormatBool(v))
		}
	}
	slices.Sort(out)
	return out, nil
}

func (r *minionSessionRepository) CloseByOperationID(ctx context.Context, operationID uuid.UUID, reason string, at time.Time) error {
	if _, err := r.coll.UpdateAll(ctx, bson.M{"operation_id": operationID, "closed_at": nil}, withRev(bson.M{"$set": bson.M{
		"closed_at": at, "close_reason": reason, "purge_at": at.Add(models.MinionSessionTombstone),
	}})); err != nil {
		return err
	}
	_, err := r.counters.RemoveAll(ctx, bson.M{"operation_id": operationID})
	return err
}

func (r *minionSessionRepository) Close(ctx context.Context, sessionID uuid.UUID, reason string, at time.Time) error {
	_, err := r.coll.UpdateAll(ctx, bson.M{"session_id": sessionID, "closed_at": nil}, withRev(bson.M{"$set": bson.M{
		"closed_at": at, "close_reason": reason, "purge_at": at.Add(models.MinionSessionTombstone),
	}}))
	return err
}

// MinionSessionClaim is what a claim of a persisted session sets.
type MinionSessionClaim struct {
	At                                             time.Time
	BuildID                                        uuid.UUID
	BuildDescription, ModuleName, Target, Platform string
	Staged                                         bool
	Crypto                                         protocol.BuildCrypto
	Hello                                          models.MinionHello
	Address                                        string
	AddressPicked                                  bool
	// Sleep is the schedule the claim's hello says; nil drops the earlier
	// process's (and its late_at) until the new one says one.
	Sleep *models.MinionSessionSleep
	// MaxTasks is the limit the claim's hello says; nil: it says none.
	// MaxTasksSettable: the claiming build's factory declares max_tasks.
	// The operator's (max_tasks_set) stays, and goes to the new process
	// when it takes one.
	MaxTasks         *int
	MaxTasksSettable bool
	// Compression is the claiming build's ("": none).
	Compression string
}

func (r *minionSessionRepository) SetArchived(ctx context.Context, sessionID uuid.UUID, archived bool) error {
	return r.coll.UpdateOne(ctx, bson.M{"session_id": sessionID}, archiveUpdate(archived))
}

// archiveUpdate archives (sets the flag) or restores (removes it).
func archiveUpdate(archived bool) bson.M {
	if archived {
		return withRev(bson.M{"$set": bson.M{"archived": true}})
	}
	return withRev(bson.M{"$unset": bson.M{"archived": ""}})
}

func (r *minionSessionRepository) SetArchivedMany(ctx context.Context, operationID uuid.UUID, ids []uuid.UUID, archived bool) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	in := make(bson.A, len(ids))
	for i, id := range ids {
		in[i] = id
	}
	res, err := r.coll.UpdateAll(ctx, bson.M{"operation_id": operationID, "session_id": bson.M{"$in": in}}, archiveUpdate(archived))
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
}

func (r *minionSessionRepository) SetArchivedMatching(ctx context.Context, operationID uuid.UUID, q MinionSessionQuery, archived bool) (int64, error) {
	res, err := r.coll.UpdateAll(ctx, minionSessionFilter(operationID, q, time.Now().UTC()), archiveUpdate(archived))
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
}

func folderUpdate(folder *uuid.UUID) bson.M {
	if folder != nil {
		return withRev(bson.M{"$set": bson.M{"folder_id": *folder}})
	}
	return withRev(bson.M{"$unset": bson.M{"folder_id": ""}})
}

func (r *minionSessionRepository) SetFolderMany(ctx context.Context, operationID uuid.UUID, ids []uuid.UUID, folder *uuid.UUID) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	in := make(bson.A, len(ids))
	for i, id := range ids {
		in[i] = id
	}
	res, err := r.coll.UpdateAll(ctx, bson.M{"operation_id": operationID, "session_id": bson.M{"$in": in}}, folderUpdate(folder))
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
}

func (r *minionSessionRepository) SetFolderMatching(ctx context.Context, operationID uuid.UUID, q MinionSessionQuery, folder *uuid.UUID) (int64, error) {
	res, err := r.coll.UpdateAll(ctx, minionSessionFilter(operationID, q, time.Now().UTC()), folderUpdate(folder))
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
}

func (r *minionSessionRepository) ClearFolder(ctx context.Context, operationID, folder uuid.UUID) (int64, error) {
	res, err := r.coll.UpdateAll(ctx, bson.M{"operation_id": operationID, "folder_id": folder}, folderUpdate(nil))
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
}

func (r *minionSessionRepository) SetDescription(ctx context.Context, sessionID uuid.UUID, description string) error {
	update := bson.M{"$set": bson.M{"description": description, "description_set": true}}
	if description == "" {
		update = bson.M{"$set": bson.M{"description_set": true}, "$unset": bson.M{"description": ""}}
	}
	return r.coll.UpdateOne(ctx, bson.M{"session_id": sessionID}, withRev(update))
}

// BackfillDescriptions is one update inside Mongo: description from
// build_description, for sessions that never had one given
// (description_set): an operator's, even cleared, stays. Idempotent.
func (r *minionSessionRepository) BackfillDescriptions(ctx context.Context) (int64, error) {
	res, err := r.coll.UpdateAll(ctx,
		bson.M{"description_set": bson.M{"$exists": false}, "build_description": bson.M{"$nin": bson.A{nil, ""}}},
		bson.A{bson.M{"$set": bson.M{"description": "$build_description", "description_set": true}}})
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
}

func (r *minionSessionRepository) BackfillProcessNames(ctx context.Context) (int64, error) {
	// The last part after a / and then after a \, as models.ProcessName.
	last := func(in any, sep string) bson.M {
		return bson.M{"$arrayElemAt": bson.A{bson.M{"$split": bson.A{in, sep}}, -1}}
	}
	res, err := r.coll.UpdateAll(ctx,
		bson.M{"hello.proc": bson.M{"$regex": `[/\\][^/\\]`}},
		bson.A{bson.M{"$set": bson.M{"hello.proc": last(last("$hello.proc", "/"), `\`)}}})
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
}

func (r *minionSessionRepository) FolderCounts(ctx context.Context, operationID uuid.UUID) (int64, map[uuid.UUID]int64, error) {
	var rows []struct {
		Folder *uuid.UUID `bson:"_id"`
		N      int64      `bson:"n"`
	}
	match := minionSessionFilter(operationID, MinionSessionQuery{}, time.Now().UTC())
	err := r.coll.Aggregate(ctx, bson.A{
		bson.M{"$match": match},
		bson.M{"$group": bson.M{"_id": "$folder_id", "n": bson.M{"$sum": 1}}},
	}).All(&rows)
	if err != nil {
		return 0, nil, err
	}
	var all int64
	by := map[uuid.UUID]int64{}
	for _, row := range rows {
		all += row.N
		if row.Folder != nil {
			by[*row.Folder] = row.N
		}
	}
	return all, by, nil
}

func (r *minionSessionRepository) SessionIDs(ctx context.Context, operationID uuid.UUID, q MinionSessionQuery, limit int64) ([]uuid.UUID, error) {
	var rows []struct {
		SessionID uuid.UUID `bson:"session_id"`
	}
	err := r.coll.Find(ctx, minionSessionFilter(operationID, q, time.Now().UTC())).
		Select(bson.M{"session_id": 1}).Limit(limit).All(&rows)
	out := make([]uuid.UUID, len(rows))
	for i, row := range rows {
		out[i] = row.SessionID
	}
	return out, err
}

func (r *minionSessionRepository) OwnSessionIDs(ctx context.Context, operationID uuid.UUID, ids []uuid.UUID) ([]uuid.UUID, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	in := make(bson.A, len(ids))
	for i, id := range ids {
		in[i] = id
	}
	var rows []struct {
		SessionID uuid.UUID `bson:"session_id"`
	}
	err := r.coll.Find(ctx, bson.M{"operation_id": operationID, "session_id": bson.M{"$in": in}}).Select(bson.M{"session_id": 1}).All(&rows)
	out := make([]uuid.UUID, len(rows))
	for i, row := range rows {
		out[i] = row.SessionID
	}
	return out, err
}

func (r *minionSessionRepository) SetPersisted(ctx context.Context, sessionID uuid.UUID) error {
	return r.coll.UpdateOne(ctx, bson.M{"session_id": sessionID}, withRev(bson.M{"$set": bson.M{"persisted": true}}))
}

func (r *minionSessionRepository) AddFeedback(ctx context.Context, sessionID uuid.UUID, errors, warnings int, at time.Time) error {
	return r.coll.UpdateOne(ctx, bson.M{"session_id": sessionID}, withRev(bson.M{
		"$inc": bson.M{"feedback_errors": errors, "feedback_warnings": warnings, "feedback_unread": errors + warnings},
		"$max": bson.M{"feedback_at": at},
	}))
}

func (r *minionSessionRepository) MarkFeedbackRead(ctx context.Context, sessionID uuid.UUID, at time.Time) error {
	return r.coll.UpdateOne(ctx, bson.M{"session_id": sessionID}, withRev(bson.M{
		"$set":   bson.M{"feedback_read_at": at},
		"$unset": bson.M{"feedback_errors": "", "feedback_warnings": "", "feedback_unread": ""},
	}))
}

func (r *minionSessionRepository) SetMaxTasks(ctx context.Context, sessionID uuid.UUID, max *int) error {
	update := bson.M{"$unset": bson.M{"max_tasks": "", "max_tasks_sends": ""}}
	if max != nil {
		update = bson.M{"$set": bson.M{"max_tasks": *max}, "$unset": bson.M{"max_tasks_sends": ""}}
	}
	return r.coll.UpdateOne(ctx, bson.M{"session_id": sessionID}, withRev(update))
}

func (r *minionSessionRepository) SetMaxTasksWanted(ctx context.Context, sessionID uuid.UUID, max int) error {
	return r.coll.UpdateOne(ctx, bson.M{"session_id": sessionID},
		withRev(bson.M{"$set": bson.M{"max_tasks_set": max}, "$unset": bson.M{"max_tasks_sends": ""}}))
}

func (r *minionSessionRepository) MaxTasksSent(ctx context.Context, sessionID uuid.UUID) error {
	return r.coll.UpdateOne(ctx, bson.M{"session_id": sessionID}, withRev(bson.M{"$inc": bson.M{"max_tasks_sends": 1}}))
}

func (r *minionSessionRepository) SetConnected(ctx context.Context, sessionID uuid.UUID, connected bool) error {
	update := withRev(bson.M{"$unset": bson.M{"connected": ""}})
	if connected {
		update = withRev(bson.M{"$set": bson.M{"connected": true}})
	}
	return r.coll.UpdateOne(ctx, bson.M{"session_id": sessionID}, update)
}

func (r *minionSessionRepository) ClearConnected(ctx context.Context) (int64, error) {
	res, err := r.coll.UpdateAll(ctx, bson.M{"connected": true}, withRev(bson.M{"$unset": bson.M{"connected": ""}}))
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
}

// Claim is one update inside Mongo: the session's keys move to the front of
// replaced_keys (MaxSessionReplacedKeys kept) as the claim's take their
// place. What the minion sent goes in through $literal, as a client's value
// must in a pipeline.
func (r *minionSessionRepository) Claim(ctx context.Context, sessionID uuid.UUID, c MinionSessionClaim) error {
	lit := func(v any) bson.M { return bson.M{"$literal": v} }
	prepend := func(field string, item any, limit int) bson.M {
		return bson.M{"$slice": bson.A{
			bson.M{"$concatArrays": bson.A{bson.A{item}, bson.M{"$ifNull": bson.A{"$" + field, bson.A{}}}}},
			limit,
		}}
	}
	restart := models.MinionSessionRestart{At: c.At, PID: c.Hello.PID, Host: c.Hello.Host, BuildID: c.BuildID}
	return r.coll.UpdateOne(ctx, bson.M{"session_id": sessionID, "closed_at": nil}, bson.A{bson.M{"$set": bson.M{
		"replaced_keys":      prepend("replaced_keys", bson.M{"crypto": "$crypto", "at": c.At}, models.MaxSessionReplacedKeys),
		"restarts":           prepend("restarts", lit(restart), models.MaxSessionRestarts),
		"restart_count":      bson.M{"$add": bson.A{bson.M{"$ifNull": bson.A{"$restart_count", 0}}, 1}},
		"crypto":             lit(c.Crypto),
		"hello":              lit(c.Hello),
		"address":            lit(c.Address),
		"address_picked":     c.AddressPicked,
		"build_id":           lit(c.BuildID),
		"build_description":  lit(c.BuildDescription),
		"module_name":        lit(c.ModuleName),
		"target":             lit(c.Target),
		"platform":           lit(c.Platform),
		"staged":             c.Staged,
		"persisted":          true,
		"sleep":              sleepOrRemove(c.Sleep),
		"max_tasks":          intOrRemove(c.MaxTasks),
		"max_tasks_settable": c.MaxTasksSettable,
		"compression":        strOrRemove(c.Compression),
		"max_tasks_sends":    "$$REMOVE",
		"late_at":            "$$REMOVE", // set again by the claim's check-in
		"lost_at":            "$$REMOVE",
		"rev":                revStage,
	}}})
}

// sleepOrRemove is a schedule for a pipeline $set, or the removal of the
// field for none.
func intOrRemove(n *int) any {
	if n == nil {
		return "$$REMOVE"
	}
	return bson.M{"$literal": *n}
}

func sleepOrRemove(s *models.MinionSessionSleep) any {
	if s == nil {
		return "$$REMOVE"
	}
	return bson.M{"$literal": s}
}

// strOrRemove is text for a pipeline $set, or the field's removal for "".
func strOrRemove(s string) any {
	if s == "" {
		return "$$REMOVE"
	}
	return bson.M{"$literal": s}
}

// BackfillLostAt is one update inside Mongo: lost_at = late_at plus the
// missed check-ins past the first, each the interval with its jitter (late_at
// already holds the first and the grace). Idempotent: only sessions without
// one.
func (r *minionSessionRepository) BackfillLostAt(ctx context.Context) (int64, error) {
	stretched := bson.M{"$multiply": bson.A{"$sleep.interval", bson.M{"$add": bson.A{1, bson.M{"$divide": bson.A{"$sleep.jitter", 100}}}}}}
	res, err := r.coll.UpdateAll(ctx,
		bson.M{"lost_at": nil, "late_at": bson.M{"$ne": nil}, "sleep.interval": bson.M{"$gt": 0}},
		bson.A{bson.M{"$set": bson.M{"lost_at": bson.M{"$add": bson.A{
			"$late_at", bson.M{"$multiply": bson.A{models.SessionLostMissed - 1, stretched}},
		}}}}})
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
}

func (r *minionSessionRepository) ByMachine(ctx context.Context, operationID uuid.UUID, machineID string) ([]models.MinionSession, error) {
	var out []models.MinionSession
	err := r.coll.Find(ctx, bson.M{"operation_id": operationID, "hello.machine_id": machineID}).Sort("-created_at").All(&out)
	return out, err
}
