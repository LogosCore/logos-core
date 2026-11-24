package factorycommands

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/credentialrefs"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// errTaskFileGone is a task file a task's args name that core no longer
// keeps: the task can never run.
var errTaskFileGone = errors.New("a task file its args name is gone")

// request is a task's factory.command payload (docs: factory.command): the
// task, its files and credentials, the session as core keeps it and its
// build, with links signed for LinkTTL (and good while the task runs).
func (s *Service) request(ctx context.Context, t *models.MinionTask, sess *models.MinionSession, b *models.Build) (protocol.FactoryCommandRequest, error) {
	expires := s.now().Add(s.cfg.LinkTTL)
	req := protocol.FactoryCommandRequest{TaskID: t.TaskID.String(), Command: t.Command, Actor: s.actor(ctx, t),
		Session: sessionRecord(sess), UploadURL: s.links.Sign(UploadPath(t.TaskID), expires)}
	if len(t.Factory.Args) > 0 {
		req.Args = map[string]any(t.Factory.Args)
	}
	build, err := s.buildRecord(ctx, t, b, expires)
	if err != nil {
		return req, err
	}
	req.Build = build
	for _, id := range t.Factory.Files {
		f, err := s.files.FileByID(ctx, id)
		if err != nil {
			return req, fmt.Errorf("task file %s: %w", id, err)
		}
		if f == nil || f.Kind != models.MinionFileKindTask || f.State != models.MinionFileComplete {
			return req, fmt.Errorf("%w (%s)", errTaskFileGone, id)
		}
		if req.Files == nil {
			req.Files = map[string]protocol.BuildFile{}
		}
		req.Files[id.String()] = taskFile(f, s.links.Sign(FilePath(t.TaskID, id), expires))
	}
	if len(t.CredentialIDs) > 0 {
		if s.creds == nil {
			return req, &credentialrefs.Unusable{Why: "core resolves no credentials here"}
		}
		if req.Credentials, _, err = s.creds.Resolve(ctx, t.OperationID, t.CredentialIDs); err != nil {
			return req, err
		}
	}
	return req, nil
}

// actor is who queued the task, by name.
func (s *Service) actor(ctx context.Context, t *models.MinionTask) *protocol.Actor {
	if t.IssuedBy == uuid.Nil {
		return nil
	}
	a := &protocol.Actor{Type: "user", ID: t.IssuedBy.String()}
	if s.users != nil {
		if users, err := s.users.FindByIDs(ctx, []uuid.UUID{t.IssuedBy}); err == nil && len(users) > 0 {
			a.Name = users[0].Username
		}
	}
	return a
}

// taskFile is a kept file as a call names it, with its link.
func taskFile(f *models.MinionFile, url string) protocol.BuildFile {
	mediaType, _, _ := strings.Cut(f.ContentType, ";")
	return protocol.BuildFile{ID: f.FileID.String(), Name: f.Name, Size: f.Total, SHA256: hex.EncodeToString(f.SHA256),
		MediaType: strings.TrimSpace(mediaType), URL: url}
}

// sessionRecord is a session as a call carries it (protocol.SessionRecord):
// what core keeps of it, but its bookkeeping, its copies of its build's
// fields, and the keys of the processes a claim replaced.
func sessionRecord(s *models.MinionSession) protocol.SessionRecord {
	h := s.Hello
	r := protocol.SessionRecord{SessionID: s.SessionID.String(), Number: s.Number, OperationID: s.OperationID.String(),
		BuildID: s.BuildID.String(), Target: s.Target, Platform: s.Platform, Description: s.Description,
		BuildDescription: s.BuildDescription, CreatedAt: s.CreatedAt, LastSeenAt: s.LastSeenAt, Staged: s.Staged,
		Hello: protocol.SessionHello{PID: h.PID, OS: h.OS, Arch: h.Arch, Host: h.Host, User: h.User, Domain: h.Domain, Proc: h.Proc,
			Integrity: h.Integrity, Addrs: h.Addrs, Version: h.Version, MachineID: h.MachineID, X: h.X, RoundtripTime: h.RoundtripTime},
		Address: s.Address, PeerAddress: s.PeerAddress, ViaSince: s.ViaSince, LateAt: s.LateAt, LostAt: s.LostAt,
		Connected: s.Connected, Conn: s.Conn, ConnKind: s.ConnKind, ConnCount: s.ConnCount,
		ConnectionsAt: s.ConnectionsAt, MaxTasks: s.MaxTasks, MaxTasksSet: s.MaxTasksSet, FeedbackErrors: s.FeedbackErrors,
		FeedbackAt: s.FeedbackAt, FeedbackReadAt: s.FeedbackReadAt, Persisted: s.Persisted, RestartCount: s.RestartCount,
		Archived: s.Archived}
	if s.FolderID != nil {
		r.FolderID = s.FolderID.String()
	}
	if a := s.Acting; a != nil {
		r.Acting = &protocol.SessionIdentity{User: a.User, Domain: a.Domain, Integrity: a.Integrity, How: a.How, Since: a.Since}
		if a.CredentialID != nil {
			r.Acting.CredentialID = a.CredentialID.String()
		}
		if a.TaskID != nil {
			r.Acting.TaskID = a.TaskID.String()
		}
	}
	for _, p := range s.Peers {
		r.Peers = append(r.Peers, protocol.SessionPeer{Address: p.Address, FirstSeen: p.FirstSeen, LastSeen: p.LastSeen, Count: p.Count})
	}
	for _, v := range s.Via {
		r.Via = append(r.Via, protocol.SessionVia{SessionID: v.SessionID.String(), Number: v.Number, TaskID: v.TaskID.String()})
	}
	if s.Sleep != nil {
		r.Sleep = &protocol.SessionSleep{Interval: s.Sleep.Interval, Jitter: s.Sleep.Jitter}
	}
	for _, c := range s.ConnChanges {
		r.ConnChanges = append(r.ConnChanges, protocol.SessionConnChange{At: c.At, From: c.From, To: c.To, Reason: c.Reason})
	}
	for _, c := range s.Connections {
		r.Connections = append(r.Connections, protocol.SessionConnection{Channel: c.Channel, Kind: c.Kind,
			Version: c.Version, Params: c.Params, Values: c.Values, FailoverAuto: c.FailoverAuto, FailAfter: c.FailAfter,
			MaxMessage: c.MaxMessage})
	}
	for _, re := range s.Restarts {
		r.Restarts = append(r.Restarts, protocol.SessionRestart{At: re.At, PID: re.PID, Host: re.Host, BuildID: re.BuildID.String()})
	}
	if s.Crypto.Suite != "" {
		c := s.Crypto
		r.Crypto = &c
	}
	return r
}

// buildRecord is a session's build as a call carries it
// (protocol.BuildRecord): core's record of it, the factory.build request its
// factory got byte for byte, and new links to its files and its artifact,
// signed until expires, for task.
func (s *Service) buildRecord(ctx context.Context, t *models.MinionTask, b *models.Build, expires time.Time) (protocol.BuildRecord, error) {
	r := protocol.BuildRecord{BuildID: b.BuildID.String(), Description: b.Description, Target: b.Target, Platform: b.Platform,
		SchemaVersion: b.SchemaVersion, CreatedBy: b.CreatedBy.Protocol(), CreatedAt: b.CreatedAt, StartedAt: b.StartedAt,
		FinishedAt: b.FinishedAt, Instance: b.Instance, FactoryVersion: b.FactoryVersion, Request: json.RawMessage(b.Request),
		ConnectionDescriptions: b.ConnectionDescriptions, CredentialRevs: b.CredentialRevs, Codec: b.Codec, Warnings: b.Warnings}
	if b.CreatedBy.ID == "" {
		r.CreatedBy = nil
	}
	for _, e := range b.ErrorCodes {
		r.ErrorCodes = append(r.ErrorCodes, protocol.BuildRecordErrorCode{
			ErrorCode: protocol.ErrorCode{Code: e.Code, Label: e.Label, Description: e.Description, Fixed: e.Fixed}, ExitCode: e.ExitCode})
	}
	if a := b.Artifact; a != nil {
		r.Artifact = &protocol.BuildRecordArtifact{BuildArtifact: protocol.BuildArtifact{Filename: a.Filename, Size: a.Size,
			SHA256: a.SHA256, MediaType: a.MediaType}, Deleted: a.Deleted}
		if !a.Deleted {
			r.Artifact.URL = s.links.Sign(ArtifactPath(t.TaskID), expires)
		}
	}
	for _, id := range b.FileIDs {
		f, err := s.buildFiles.File(ctx, id.String())
		if err != nil {
			return r, fmt.Errorf("build file %s: %w", id, err)
		}
		if r.Files == nil {
			r.Files = map[string]protocol.BuildFile{}
		}
		pf := f.Protocol()
		pf.URL = s.links.Sign(BuildFilePath(t.TaskID, id), expires)
		r.Files[pf.ID] = pf
	}
	return r, nil
}
