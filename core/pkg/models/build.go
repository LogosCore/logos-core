package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"github.com/qiniu/qmgo/field"
)

// Minion builds (contracts/minion-build.md). A factory declares a build
// schema at module.register (Module.Build). An operation keeps build files
// (uploads for file fields) and its builds: each asks a factory instance for
// one minion, and a successful one creates the Minion.

// BuildFile is a file an operator uploaded for a build's file fields, such as
// an icon or a signing certificate, kept by an operation. Its bytes are in the
// build bucket under ObjectKey and never change. Values name it by FileID;
// a build hands the factory a presigned URL to it.
type BuildFile struct {
	field.DefaultField `bson:",inline"`

	FileID      uuid.UUID   `bson:"file_id" json:"file_id"`
	OperationID uuid.UUID   `bson:"operation_id" json:"operation_id"`
	Name        string      `bson:"name" json:"name"`
	Size        int64       `bson:"size" json:"size"`
	SHA256      string      `bson:"sha256" json:"sha256"`
	MediaType   string      `bson:"media_type,omitempty" json:"media_type,omitempty"`
	ObjectKey   string      `bson:"object_key" json:"-"`
	UploadedBy  ModuleActor `bson:"uploaded_by" json:"uploaded_by"`
	CreatedAt   time.Time   `bson:"created_at" json:"created_at"`
}

// Protocol is the file as a build request carries it, without its URL.
func (f BuildFile) Protocol() protocol.BuildFile {
	return protocol.BuildFile{ID: f.FileID.String(), Name: f.Name, Size: f.Size, SHA256: f.SHA256, MediaType: f.MediaType}
}

// Build states.
const (
	BuildStateQueued    = "QUEUED"
	BuildStateBuilding  = "BUILDING"
	BuildStateSucceeded = "SUCCEEDED"
	BuildStateFailed    = "FAILED"
	BuildStateCancelled = "CANCELLED"
)

// Reasons core fails a build for, next to the factory's own error codes.
const (
	BuildErrorTimeout          = "timeout"
	BuildErrorFactoryLost      = "factory_lost"
	BuildErrorArtifactMismatch = "artifact_mismatch"
	BuildErrorSchemaChanged    = "schema_changed"
	BuildErrorRefused          = "refused"            // the factory would not take the build
	BuildErrorArtifactTooLarge = "artifact_too_large" // over the max_size the factory was given
	// BuildErrorCredentials: a credential the build names is gone, or not
	// of a type its field takes any more (converted since).
	BuildErrorCredentials = "credentials"
	// BuildErrorMinionConfig: the minion config its factory asks for cannot
	// be made (protocol.MinionConfig): a channel that numbers no fields of the
	// build's connection kind, or that follows another schema version now.
	BuildErrorMinionConfig = "minion_config"
)

// Build is one build: what was asked for, with defaults filled in (the build
// record), its progress and its outcome.
type Build struct {
	field.DefaultField `bson:",inline"`

	// BuildID is the id of the build and of the minion it makes: factories
	// know the build by it, and the minion sends it on the data plane.
	BuildID     uuid.UUID `bson:"build_id" json:"build_id"`
	OperationID uuid.UUID `bson:"operation_id" json:"operation_id"`
	// ModuleName is the factory kind.
	ModuleName string `bson:"module_name" json:"module_name"`
	// Description is what the build is for: the operators' note, core's own,
	// never in the build request. Empty for a build made before it was
	// required.
	Description   string           `bson:"description,omitempty" json:"description,omitempty"`
	Target        string           `bson:"target" json:"target"`
	Platform      string           `bson:"platform" json:"platform"`
	SchemaVersion int              `bson:"schema_version" json:"schema_version"`
	Values        JSONMap          `bson:"values" json:"values"`
	Connections   BuildConnections `bson:"connections" json:"connections"`
	// ConnectionDescriptions are the operators' notes on the connections, by
	// position ("" for none). Core's own: kept out of Connections, which is
	// what the factory gets, so a note never ends up in an artifact.
	ConnectionDescriptions []string    `bson:"connection_descriptions,omitempty" json:"connection_descriptions,omitempty"`
	FileIDs                []uuid.UUID `bson:"file_ids,omitempty" json:"file_ids,omitempty"`
	// CredentialIDs are the credentials the values, the connections'
	// values and their endpoints' params name: the factory gets them with
	// the build. CredentialRevs is the revision of each that went to the
	// factory, by id, once one took the build.
	CredentialIDs  []uuid.UUID      `bson:"credential_ids,omitempty" json:"credential_ids,omitempty"`
	CredentialRevs map[string]int64 `bson:"credential_revs,omitempty" json:"credential_revs,omitempty"`
	// Commands are what the minion can do: the commands the operator picked,
	// with the target's required ones, in the schema's order. The factory
	// gets them, and the minion has these and no others.
	Commands []string `bson:"commands,omitempty" json:"commands,omitempty"`
	// Crypto is the minion's payload crypto suite and keys, generated by core
	// when its factory declares a suite. It goes to the factory, into the
	// artifact, and to the build's JSON tab through GraphQL.
	Crypto *protocol.BuildCrypto `bson:"crypto,omitempty" json:"-"`
	// PersistedSessionID is the session the minion claims in its hello
	// (protocol.BuildRequest.SessionID); nil for a minion that opens new
	// sessions. One per build.
	PersistedSessionID *uuid.UUID `bson:"persisted_session_id,omitempty" json:"persisted_session_id,omitempty"`
	// Failover is how the minion moves between its connections on its own
	// (protocol.BuildFailover), as the factory gets it; nil for a build made
	// before it existed (FailoverPolicy: the defaults).
	Failover *BuildFailover `bson:"failover,omitempty" json:"failover,omitempty"`
	// MaxTasks is how many tasks the minion runs at once as it starts
	// (protocol.BuildRequest.MaxTasks), for a factory whose minions take
	// one (protocol.BuildSchema.MaxTasks); nil for any other.
	MaxTasks *int `bson:"max_tasks,omitempty" json:"max_tasks,omitempty"`
	// Compression is the algorithm the minion packs and inflates
	// (protocol.CompressionDeflate), for a factory that declares
	// protocol.BuildSchema.Compression: on unless the operator turned it
	// off; "" for off, or a factory that declares none.
	Compression string `bson:"compression,omitempty" json:"compression,omitempty"`
	// ErrorCodes are what the minion's exit codes mean: its factory's
	// (protocol.ErrorCode) as they were when the build was queued, in the
	// factory's order, each with this build's exit code. Empty for a
	// factory that declares none, and for a build queued before builds kept
	// them.
	ErrorCodes []BuildErrorCode `bson:"error_codes,omitempty" json:"error_codes,omitempty"`
	// UniqueErrorCodes says core drew the build's exit codes
	// (protocol.DrawErrorCodes), for a factory that declares
	// protocol.BuildSchema.ErrorCodesPerBuild, unless the operator turned
	// that off: the request carries them (DrawnErrorCodes).
	UniqueErrorCodes bool `bson:"unique_error_codes,omitempty" json:"unique_error_codes,omitempty"`
	// Envelope is how the minion writes its messages (its target's,
	// protocol.BuildSchema.TargetCodec): core's protocol.EnvelopeJSON, or a
	// factory's own; "" for core's CBOR. Its suite is Crypto's.
	Envelope string `bson:"envelope,omitempty" json:"envelope,omitempty"`
	// Codec is what the factory declared of the layers it translates
	// (protocol.BuildSchema.Codec) when the build was made, for a build
	// whose suite or envelope is the factory's own; nil for core's codec.
	Codec *protocol.BuildCodec `bson:"codec,omitempty" json:"codec,omitempty"`
	// SearchText is what the builds list search reads of the build beyond
	// its fields (BuildSearchText), set when the build is stored.
	SearchText string `bson:"search_text" json:"-"`

	State string `bson:"state" json:"state"`
	// Instance is the factory instance building it (set when it accepted).
	Instance       string `bson:"instance,omitempty" json:"instance,omitempty"`
	FactoryVersion string `bson:"factory_version,omitempty" json:"factory_version,omitempty"`
	// LeaseUntil claims a queued build for one core instance while it offers
	// the build to factories.
	LeaseUntil *time.Time `bson:"lease_until,omitempty" json:"-"`
	// Deadline is when core stops waiting: for a factory to take the build
	// while queued, for the result once building.
	Deadline time.Time `bson:"deadline" json:"deadline"`
	// ArtifactMaxSize is the max_size the instance building it was given:
	// a result reporting a larger artifact fails the build. Zero for a
	// build offered before core kept it.
	ArtifactMaxSize int64 `bson:"artifact_max_size,omitempty" json:"-"`
	// Request is the factory.build payload core sent Instance, byte for
	// byte: the protocol.BuildRequest JSON that went as the message's
	// payload, kept with Instance when it is offered there and dropped
	// with it when that instance does not take the build. The build's
	// JSON tab shows it as what went to the factory. It holds what the
	// request does, the minion's keys and its credentials' secrets among
	// them. Empty for a build never offered, or offered before core kept
	// it.
	Request string `bson:"request,omitempty" json:"-"`
	// ResultReceived is set when a succeeded result arrived and its artifact
	// is being checked; ResultAt is when.
	ResultReceived bool       `bson:"result_received,omitempty" json:"-"`
	ResultAt       *time.Time `bson:"result_at,omitempty" json:"result_at,omitempty"`

	// Plan is the factory's plan for the build, from the instance's
	// acceptance: its steps, in order. Empty until then.
	Plan []BuildPlanStep `bson:"plan,omitempty" json:"plan,omitempty"`
	// StepReports are the factory's progress reports by step id: when it
	// first said it was on the step, and its latest message. They may
	// arrive before the plan, and a step outside the plan is never shown.
	StepReports map[string]BuildStepReport `bson:"step_reports,omitempty" json:"step_reports,omitempty"`

	Artifact *BuildArtifact `bson:"artifact,omitempty" json:"artifact,omitempty"`
	Warnings []string       `bson:"warnings,omitempty" json:"warnings,omitempty"`
	// Log is the output the factory sent with its result: kept only when
	// it sent no live log (LogStreamed), which is in the build log chunks
	// instead.
	Log string `bson:"log,omitempty" json:"log,omitempty"`
	// LogStreamed is set by the factory's first live log chunk
	// (factory.build.log); LogBytes is the text core keeps of it, and
	// LogTrimmed the lines it dropped from its start to keep within
	// builds.MaxBuildLogBytes.
	LogStreamed bool        `bson:"log_streamed,omitempty" json:"-"`
	LogBytes    int64       `bson:"log_bytes,omitempty" json:"-"`
	LogTrimmed  int64       `bson:"log_trimmed,omitempty" json:"-"`
	Error       *BuildError `bson:"error,omitempty" json:"error,omitempty"`

	CreatedBy   ModuleActor  `bson:"created_by" json:"created_by"`
	CancelledBy *ModuleActor `bson:"cancelled_by,omitempty" json:"cancelled_by,omitempty"`
	CreatedAt   time.Time    `bson:"created_at" json:"created_at"`
	StartedAt   *time.Time   `bson:"started_at,omitempty" json:"started_at,omitempty"`
	FinishedAt  *time.Time   `bson:"finished_at,omitempty" json:"finished_at,omitempty"`
}

// Finished reports whether the build reached a final state.
func (b Build) Finished() bool {
	switch b.State {
	case BuildStateSucceeded, BuildStateFailed, BuildStateCancelled:
		return true
	}
	return false
}

// BuildConnections are a build's connections as the factory got them: each
// one's settings as an endpoint export, and the connection parameter values.
// Stored JSON-faithfully, like schemas.
type BuildConnections struct {
	Items []protocol.BuildConnection `json:"items"`
}

func (c BuildConnections) MarshalBSON() ([]byte, error) {
	if c.Items == nil {
		c.Items = []protocol.BuildConnection{}
	}
	return jsonToBSONDoc(c)
}

func (c *BuildConnections) UnmarshalBSON(data []byte) error {
	return bsonDocToJSON(data, c)
}

// BuildFailover is a build's failover policy, stored JSON-faithfully like
// its connections.
type BuildFailover protocol.BuildFailover

func (f BuildFailover) MarshalBSON() ([]byte, error) { return jsonToBSONDoc(protocol.BuildFailover(f)) }

func (f *BuildFailover) UnmarshalBSON(data []byte) error {
	var p protocol.BuildFailover
	if err := bsonDocToJSON(data, &p); err != nil {
		return err
	}
	*f = BuildFailover(p)
	return nil
}

// FailoverPolicy is the build's failover policy: its own, or the defaults
// for a build made before builds had one.
func (b *Build) FailoverPolicy() protocol.BuildFailover {
	if b.Failover == nil {
		return protocol.DefaultBuildFailover()
	}
	return protocol.BuildFailover(*b.Failover)
}

// BuildPlanStep is one step of the factory's plan for a build. Its id is a
// slug without dots, so it keys StepReports.
type BuildPlanStep struct {
	ID    string `bson:"id" json:"id"`
	Label string `bson:"label" json:"label"`
}

// BuildStepReport is what the factory reported of one step.
type BuildStepReport struct {
	// StartedAt is when the factory first said it was on the step.
	StartedAt time.Time `bson:"started_at" json:"started_at"`
	Message   string    `bson:"message,omitempty" json:"message,omitempty"`
}

// BuildLogChunk is a chunk of a build's live log as its factory sent it
// (factory.build.log): its lines, each with its plan step, numbered by Seq.
type BuildLogChunk struct {
	BuildID     uuid.UUID      `bson:"build_id"`
	OperationID uuid.UUID      `bson:"operation_id"`
	Seq         int64          `bson:"seq"`
	Lines       []BuildLogLine `bson:"lines"`
	// Dropped is how many lines the factory dropped before these.
	Dropped int64 `bson:"dropped,omitempty"`
	// Bytes is the lines' text, all together.
	Bytes      int64     `bson:"bytes"`
	ReceivedAt time.Time `bson:"received_at"`
}

// BuildLogLine is one line of a build's live log.
type BuildLogLine struct {
	Step string    `bson:"step,omitempty"`
	At   time.Time `bson:"at"`
	Text string    `bson:"text"`
}

// BuildArtifact is a build's artifact: what the factory reported, checked
// against what the store holds under ObjectKey. Deleted is set once an
// operator removed the bytes; the record stays.
type BuildArtifact struct {
	Filename  string `bson:"filename" json:"filename"`
	Size      int64  `bson:"size" json:"size"`
	SHA256    string `bson:"sha256" json:"sha256"`
	MediaType string `bson:"media_type,omitempty" json:"media_type,omitempty"`
	ObjectKey string `bson:"object_key" json:"-"`
	Deleted   bool   `bson:"deleted,omitempty" json:"deleted,omitempty"`
}

// BuildError is why a build failed: the factory's code, or core's own
// (BuildError* constants).
type BuildError struct {
	Code    string `bson:"code" json:"code"`
	Message string `bson:"message" json:"message"`
}

// Minion is what a successful build creates: an identity core knows on the
// data plane. Its id is its build's. Its keys come with the payload crypto
// contract.
type Minion struct {
	field.DefaultField `bson:",inline"`

	BuildID     uuid.UUID `bson:"build_id" json:"build_id"`
	OperationID uuid.UUID `bson:"operation_id" json:"operation_id"`
	ModuleName  string    `bson:"module_name" json:"module_name"`
	Target      string    `bson:"target" json:"target"`
	Platform    string    `bson:"platform" json:"platform"`
	// Commands are what the minion can do: its build's.
	Commands  []string    `bson:"commands,omitempty" json:"commands,omitempty"`
	CreatedBy ModuleActor `bson:"created_by" json:"created_by"`
	CreatedAt time.Time   `bson:"created_at" json:"created_at"`
}

// BuildErrorCode is one of a build's error codes: one its factory declares
// (protocol.ErrorCode), with the code the build's minion exits with.
type BuildErrorCode struct {
	// Code is the declared code.
	Code int `bson:"code" json:"code"`
	// ExitCode is what the build's minion exits with for it: drawn for the
	// build (Build.UniqueErrorCodes), or Code.
	ExitCode    int    `bson:"exit_code" json:"exit_code"`
	Label       string `bson:"label" json:"label"`
	Description string `bson:"description,omitempty" json:"description,omitempty"`
	// Fixed: the same in every build.
	Fixed bool `bson:"fixed,omitempty" json:"fixed,omitempty"`
}

// NewBuildErrorCodes is a build's error codes: declared, its factory's,
// each with the exit code drawn for it (protocol.DrawErrorCodes), or its
// own where none was.
func NewBuildErrorCodes(declared []protocol.ErrorCode, drawn []protocol.BuildErrorCode) []BuildErrorCode {
	if len(declared) == 0 {
		return nil
	}
	exits := make(map[int]int, len(drawn))
	for _, d := range drawn {
		exits[d.Code] = d.ExitCode
	}
	out := make([]BuildErrorCode, len(declared))
	for i, e := range declared {
		exit, ok := exits[e.Code]
		if !ok {
			exit = e.Code
		}
		out[i] = BuildErrorCode{Code: e.Code, ExitCode: exit, Label: e.Label, Description: e.Description, Fixed: e.Fixed}
	}
	return out
}

// DrawnErrorCodes are the exit codes core drew for b, as its factory gets
// them (protocol.BuildRequest.ErrorCodes); nil when it drew none.
func (b *Build) DrawnErrorCodes() []protocol.BuildErrorCode {
	if !b.UniqueErrorCodes {
		return nil
	}
	var out []protocol.BuildErrorCode
	for _, e := range b.ErrorCodes {
		if !e.Fixed {
			out = append(out, protocol.BuildErrorCode{Code: e.Code, ExitCode: e.ExitCode})
		}
	}
	return out
}
