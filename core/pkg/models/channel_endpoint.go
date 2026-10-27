package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/qiniu/qmgo/field"
)

// ChannelEndpoint is a ready-to-use set of values for a channel kind's minion
// connection schema (Module.Connection), e.g. the base URL of a CDN in front
// of the HTTP channels, kept by an operation for its operators to reuse when
// creating minions. The values are copied into a minion, so nothing depends
// on an endpoint afterwards, and channels never see them.
//
// An endpoint belongs to one operation and one channel kind (ModuleName), not
// to an instance: one address often fronts several instances, and instances
// come and go. Values are validated against the kind's latest schema when
// saved and again when read, since a newer release may declare a different
// one. They are operation data, shown to members in clear text (secret
// fields included).
type ChannelEndpoint struct {
	field.DefaultField `bson:",inline"`

	EndpointID  uuid.UUID `bson:"endpoint_id" json:"endpoint_id"`
	OperationID uuid.UUID `bson:"operation_id" json:"operation_id"`
	// Name is unique within the operation.
	Name        string `bson:"name" json:"name"`
	Description string `bson:"description,omitempty" json:"description,omitempty"`
	// ModuleName is the channel kind (Module.Name, e.g. "http").
	ModuleName string `bson:"module_name" json:"module_name"`
	// SchemaVersion is the connection schema version Values were saved
	// against.
	SchemaVersion int     `bson:"schema_version" json:"schema_version"`
	Values        JSONMap `bson:"values" json:"values"`
	// SearchText is the values as the endpoints list search reads them
	// (EndpointSearchText), set whenever they are stored.
	SearchText string `bson:"search_text" json:"-"`
	// Revision grows with every write; updates compare against it.
	Revision  int64       `bson:"revision" json:"revision"`
	CreatedBy ModuleActor `bson:"created_by" json:"created_by"`
	UpdatedBy ModuleActor `bson:"updated_by" json:"updated_by"`
	CreatedAt time.Time   `bson:"created_at" json:"created_at"`
	UpdatedAt time.Time   `bson:"updated_at" json:"updated_at"`
}
