package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"github.com/qiniu/qmgo/field"
)

// MinionStaging is what a minion's stage leaves for its next message: the
// temporary id core gave it, and the new keys for its build's suite, which
// the minion got encrypted to its own RSA key. It lives a day (a TTL index
// on CreatedAt), and serves the minion's hello under the temporary id.
type MinionStaging struct {
	field.DefaultField `bson:",inline"`

	TempID      uuid.UUID `bson:"temp_id" json:"temp_id"`
	BuildID     uuid.UUID `bson:"build_id" json:"build_id"`
	OperationID uuid.UUID `bson:"operation_id" json:"operation_id"`
	// Crypto is the suite and the new key material. Never shown.
	Crypto    protocol.BuildCrypto `bson:"crypto" json:"-"`
	CreatedAt time.Time            `bson:"created_at" json:"created_at"`
}
