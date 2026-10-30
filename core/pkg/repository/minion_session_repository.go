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
	// SetDescription replaces a session's description; IsNotFound when
	// there is none.
	SetDescription(ctx context.Context, sessionID uuid.UUID, description string) error
	// BackfillDescriptions gives sessions from before they had a
	// description their build's.
	BackfillDescriptions(ctx context.Context) (int64, error)
	// SetPersisted marks a session persisted: a build names it.
	SetPersisted(ctx context.Context, sessionID uuid.UUID) error
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
}

type minionSessionRepository struct{ coll, counters database.Collection }

func NewMinionSessionRepository(db database.Database) IMinionSessionRepository {
	db.EnsureIndexes(context.Background(), minionSessionCollection, []opts.IndexModel{
		{Key: []string{"session_id"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
		{Key: []string{"operation_id", "number"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
		{Key: []string{"operation_id", "-created_at", "-_id"}},
		{Key: []string{"operation_id", "late_at"}},
		{Key: []string{"operation_id", "lost_at"}},
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

func (r *minionSessionRepository) Insert(ctx context.Context, s *models.MinionSession) error {
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
	if c.Peer == "" {
		return r.coll.UpdateOne(ctx, bson.M{"session_id": sessionID}, bson.M{"$set": set, "$unset": bson.M{"purge_at": "", "archived": ""}})
	}
	return r.coll.UpdateOne(ctx, bson.M{"session_id": sessionID}, checkInPipeline(c.At, c.Peer, set))
}

// checkInPipeline is CheckIn's update with a peer address, in one write,
// with the check-in's other fields (set):
//
//	old   = the peers entry for peer, if any
//	peers = slice(sort(peers without it + {peer, old.first_seen ?? at, at,
//	        old.count+1}, by last_seen desc), MaxSessionPeers)
//
// The address goes in through $literal, as a client's value must.
func checkInPipeline(at time.Time, peer string, set bson.M) bson.A {
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
	return bson.A{bson.M{"$set": stage}, bson.M{"$unset": bson.A{"purge_at", "archived"}}}
}

func (r *minionSessionRepository) SetHello(ctx context.Context, sessionID uuid.UUID, hello models.MinionHello, address string, picked bool) error {
	return r.coll.UpdateOne(ctx, bson.M{"session_id": sessionID},
		bson.M{"$set": bson.M{"hello": hello, "address": address, "address_picked": picked}})
}

func (r *minionSessionRepository) SetAddress(ctx context.Context, sessionID uuid.UUID, address string, picked bool) error {
	filter := bson.M{"session_id": sessionID}
	if address != "" {
		filter["hello.addrs"] = address
	}
	return r.coll.UpdateOne(ctx, filter, bson.M{"$set": bson.M{"address": address, "address_picked": picked}})
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
		"hello.host", "hello.user", "hello.domain", "hello.proc", "hello.os", "hello.arch", "hello.integrity",
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
	if _, err := r.coll.UpdateAll(ctx, bson.M{"operation_id": operationID, "closed_at": nil}, bson.M{"$set": bson.M{
		"closed_at": at, "close_reason": reason, "purge_at": at.Add(models.MinionSessionTombstone),
	}}); err != nil {
		return err
	}
	_, err := r.counters.RemoveAll(ctx, bson.M{"operation_id": operationID})
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
}

func (r *minionSessionRepository) SetArchived(ctx context.Context, sessionID uuid.UUID, archived bool) error {
	return r.coll.UpdateOne(ctx, bson.M{"session_id": sessionID}, archiveUpdate(archived))
}

// archiveUpdate archives (sets the flag) or restores (removes it).
func archiveUpdate(archived bool) bson.M {
	if archived {
		return bson.M{"$set": bson.M{"archived": true}}
	}
	return bson.M{"$unset": bson.M{"archived": ""}}
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

func (r *minionSessionRepository) SetDescription(ctx context.Context, sessionID uuid.UUID, description string) error {
	update := bson.M{"$set": bson.M{"description": description, "description_set": true}}
	if description == "" {
		update = bson.M{"$set": bson.M{"description_set": true}, "$unset": bson.M{"description": ""}}
	}
	return r.coll.UpdateOne(ctx, bson.M{"session_id": sessionID}, update)
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

func (r *minionSessionRepository) SetPersisted(ctx context.Context, sessionID uuid.UUID) error {
	return r.coll.UpdateOne(ctx, bson.M{"session_id": sessionID}, bson.M{"$set": bson.M{"persisted": true}})
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
		"replaced_keys":     prepend("replaced_keys", bson.M{"crypto": "$crypto", "at": c.At}, models.MaxSessionReplacedKeys),
		"restarts":          prepend("restarts", lit(restart), models.MaxSessionRestarts),
		"restart_count":     bson.M{"$add": bson.A{bson.M{"$ifNull": bson.A{"$restart_count", 0}}, 1}},
		"crypto":            lit(c.Crypto),
		"hello":             lit(c.Hello),
		"address":           lit(c.Address),
		"address_picked":    c.AddressPicked,
		"build_id":          lit(c.BuildID),
		"build_description": lit(c.BuildDescription),
		"module_name":       lit(c.ModuleName),
		"target":            lit(c.Target),
		"platform":          lit(c.Platform),
		"staged":            c.Staged,
		"persisted":         true,
		"sleep":             sleepOrRemove(c.Sleep),
		"late_at":           "$$REMOVE", // set again by the claim's check-in
		"lost_at":           "$$REMOVE",
	}}})
}

// sleepOrRemove is a schedule for a pipeline $set, or the removal of the
// field for none.
func sleepOrRemove(s *models.MinionSessionSleep) any {
	if s == nil {
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
