package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"github.com/qiniu/qmgo/field"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Minion builds (contracts/minion-build.md). A factory declares a build
// schema at module.register (Module.Build). An operation keeps build files
// (uploads for file fields) and presets (ready-to-use build settings), and its
// builds: each asks a factory instance for one minion, and a successful one
// creates the Minion.

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

// BuildPreset is ready-to-use build settings an operation keeps, the way an
// endpoint is ready-to-use connection values: it fills in the build form and
// builds nothing itself. Its connections hold their own settings (filling one
// from an endpoint copies the endpoint's values), and it is validated against
// the factory kind's and the channels' latest schemas when saved and read.
type BuildPreset struct {
	field.DefaultField `bson:",inline"`

	OperationID uuid.UUID `bson:"operation_id" json:"operation_id"`
	// Name is unique within the operation.
	Name        string `bson:"name" json:"name"`
	Description string `bson:"description,omitempty" json:"description,omitempty"`
	// ModuleName is the factory kind (Module.Name, e.g. "bob").
	ModuleName string `bson:"module_name" json:"module_name"`
	Target     string `bson:"target" json:"target"`
	// Platform is optional: it may be left for the build form.
	Platform string `bson:"platform,omitempty" json:"platform,omitempty"`
	// SchemaVersion is the build schema version Values were saved against.
	SchemaVersion int                     `bson:"schema_version" json:"schema_version"`
	Values        JSONMap                 `bson:"values" json:"values"`
	Connections   []BuildPresetConnection `bson:"connections" json:"connections"`
	// FileIDs are the build files the values name: what keeps them stored.
	FileIDs []string `bson:"file_ids,omitempty" json:"file_ids,omitempty"`
	// Revision grows with every write; updates compare against it.
	Revision  int64       `bson:"revision" json:"revision"`
	CreatedBy ModuleActor `bson:"created_by" json:"created_by"`
	UpdatedBy ModuleActor `bson:"updated_by" json:"updated_by"`
	CreatedAt time.Time   `bson:"created_at" json:"created_at"`
	UpdatedAt time.Time   `bson:"updated_at" json:"updated_at"`
}

// BuildPresetConnection is one connection of a preset: a channel kind, its
// connection settings and the factory's connection parameter values for it.
type BuildPresetConnection struct {
	// ModuleName is the channel kind (e.g. "http").
	ModuleName string `bson:"module_name" json:"module_name"`
	// Name is what the minion calls the connection; empty, it is named by
	// channel and position when building.
	Name string `bson:"name,omitempty" json:"name,omitempty"`
	// Description is the operators' note on the connection; builds keep it
	// in ConnectionDescriptions, never in what the factory gets.
	Description string `bson:"description,omitempty" json:"description,omitempty"`
	// SchemaVersion is the channel's connection schema version Settings
	// were saved against.
	SchemaVersion int     `bson:"schema_version" json:"schema_version"`
	Settings      JSONMap `bson:"settings" json:"settings"`
	Values        JSONMap `bson:"values" json:"values"`
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
	BuildErrorRefused          = "refused" // the factory would not take the build
)

// Build is one build: what was asked for, with defaults filled in (the build
// record), its progress and its outcome.
type Build struct {
	field.DefaultField `bson:",inline"`

	// BuildID is the ULID factories know the build by.
	BuildID     string    `bson:"build_id" json:"build_id"`
	OperationID uuid.UUID `bson:"operation_id" json:"operation_id"`
	// ModuleName is the factory kind.
	ModuleName    string           `bson:"module_name" json:"module_name"`
	Target        string           `bson:"target" json:"target"`
	Platform      string           `bson:"platform" json:"platform"`
	SchemaVersion int              `bson:"schema_version" json:"schema_version"`
	Values        JSONMap          `bson:"values" json:"values"`
	Connections   BuildConnections `bson:"connections" json:"connections"`
	// ConnectionDescriptions are the operators' notes on the connections, by
	// position ("" for none). Core's own: kept out of Connections, which is
	// what the factory gets, so a note never ends up in an artifact.
	ConnectionDescriptions []string `bson:"connection_descriptions,omitempty" json:"connection_descriptions,omitempty"`
	FileIDs                []string `bson:"file_ids,omitempty" json:"file_ids,omitempty"`
	// MinionID is the UUIDv4 the minion will send on the data plane.
	MinionID string `bson:"minion_id" json:"minion_id"`
	// PresetID is the preset the build was started from, if any.
	PresetID *primitive.ObjectID `bson:"preset_id,omitempty" json:"preset_id,omitempty"`

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
	// ResultReceived is set when a succeeded result arrived and its artifact
	// is being checked.
	ResultReceived bool `bson:"result_received,omitempty" json:"-"`

	Steps        []BuildStep    `bson:"steps,omitempty" json:"steps,omitempty"`
	Artifact     *BuildArtifact `bson:"artifact,omitempty" json:"artifact,omitempty"`
	Capabilities []string       `bson:"capabilities,omitempty" json:"capabilities,omitempty"`
	Warnings     []string       `bson:"warnings,omitempty" json:"warnings,omitempty"`
	Log          string         `bson:"log,omitempty" json:"log,omitempty"`
	Error        *BuildError    `bson:"error,omitempty" json:"error,omitempty"`

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

// BuildStep is one progress report of a running build.
type BuildStep struct {
	Step    string    `bson:"step" json:"step"`
	Message string    `bson:"message,omitempty" json:"message,omitempty"`
	Percent *int      `bson:"percent,omitempty" json:"percent,omitempty"`
	At      time.Time `bson:"at" json:"at"`
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
// data plane, with its build. Its keys come with the payload crypto contract.
type Minion struct {
	field.DefaultField `bson:",inline"`

	MinionID     string      `bson:"minion_id" json:"minion_id"`
	OperationID  uuid.UUID   `bson:"operation_id" json:"operation_id"`
	BuildID      string      `bson:"build_id" json:"build_id"`
	ModuleName   string      `bson:"module_name" json:"module_name"`
	Target       string      `bson:"target" json:"target"`
	Platform     string      `bson:"platform" json:"platform"`
	Capabilities []string    `bson:"capabilities,omitempty" json:"capabilities,omitempty"`
	CreatedBy    ModuleActor `bson:"created_by" json:"created_by"`
	CreatedAt    time.Time   `bson:"created_at" json:"created_at"`
}
