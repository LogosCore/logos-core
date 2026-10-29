// Package minionsync opens what a minion sends through a channel and answers
// it (docs: contracts/minion-messages.md). Built so far: staging. A minion's
// first message, sealed with its build's keys and sent under its build id,
// carries a stage: its RSA public key and a correlation id. Core answers with
// a temporary id and new keys for the build's suite, encrypted to that key,
// and keeps both for the minion's next message.
package minionsync

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/cache"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/payloadcrypto"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// ErrUnopened means the message did not open: no keys for its id, or a
// frame that fails its checks. The sync call answers 422, and nothing more:
// core cannot tell who sent it.
var ErrUnopened = errors.New("the message does not open")

// replayWindow is how long core remembers a minion message's id.
const replayWindow = 24 * time.Hour

const replayKeyPrefix = "minion:msg:"

// Builds is what the service reads of builds.
type Builds interface {
	FindByBuildID(ctx context.Context, buildID uuid.UUID) (*models.Build, error)
}

// Service handles minion messages.
type Service struct {
	builds   Builds
	stagings repository.IMinionStagingRepository
	cache    cache.Cache
	logger   *zap.Logger
	now      func() time.Time
}

func NewService(builds Builds, stagings repository.IMinionStagingRepository, c cache.Cache, logger *zap.Logger) *Service {
	return &Service{builds: builds, stagings: stagings, cache: c, logger: logger.Named("minionsync"),
		now: func() time.Time { return time.Now().UTC() }}
}

// Handle opens a minion's sealed frame sent under id and returns core's
// sealed answer: nil when there is nothing to say (the sync call's 204).
func (s *Service) Handle(ctx context.Context, id string, frame []byte) ([]byte, error) {
	sentUnder, err := uuid.Parse(id)
	if err != nil {
		return nil, ErrUnopened
	}
	b, keys, err := s.buildKeys(ctx, sentUnder)
	if err != nil {
		return nil, err
	}
	plain, err := protocol.OpenFrame(keys, sentUnder, protocol.FrameMinionToCore, frame)
	if err != nil {
		s.logger.Info("minion message does not open", zap.String("build_id", id))
		return nil, ErrUnopened
	}
	var msg protocol.MinionMessage
	if err := protocol.UnmarshalCBOR(plain, &msg); err != nil {
		s.logger.Info("minion message is not CBOR", zap.String("build_id", id), zap.Error(err))
		return nil, ErrUnopened
	}
	if err := protocol.ValidateMessageHead(msg.V, msg.ID, msg.Re, len(msg.Items)); err != nil || msg.ID == nil {
		s.logger.Info("minion message head", zap.String("build_id", id), zap.Error(err))
		return nil, ErrUnopened
	}

	fresh, err := s.cache.SetNX(ctx, replayKeyPrefix+hex.EncodeToString(msg.ID), "1", replayWindow)
	if err != nil {
		s.logger.Warn("minion replay check failed, handling the message anyway", zap.Error(err))
	} else if !fresh {
		s.logger.Info("minion message replayed, dropped", zap.String("build_id", id))
		return nil, nil
	}

	items, err := s.underBuild(ctx, b, msg)
	if err != nil {
		return nil, err
	}
	reply, err := protocol.MarshalCBOR(protocol.CoreMessage{V: protocol.MinionMessageV1, Re: msg.ID, Items: items})
	if err != nil {
		return nil, err
	}
	return protocol.SealFrame(keys, sentUnder, protocol.FrameCoreToMinion, reply)
}

// underBuild answers a message sent under a build id: only a stage is taken
// there (a hello straight under the build id is not built yet).
func (s *Service) underBuild(ctx context.Context, b *models.Build, msg protocol.MinionMessage) ([]cbor.RawMessage, error) {
	if len(msg.Items) != 1 || protocol.ItemType(msg.Items[0]) != protocol.ItemStage {
		return errItem(protocol.ErrItemBadMessage, "under a build id the only item taken is one stage", 0)
	}
	var stage protocol.StageItem
	if err := protocol.UnmarshalCBOR(msg.Items[0], &stage); err != nil {
		return errItem(protocol.ErrItemBadMessage, "the stage does not read", 0)
	}
	pub, err := protocol.ParseStageKey(stage.Key)
	if err != nil {
		return errItem(protocol.ErrItemBadMessage, err.Error(), 0)
	}
	if len(stage.Corr) != 16 {
		return errItem(protocol.ErrItemBadMessage, "corr is 16 bytes", 0)
	}

	fresh, err := payloadcrypto.NewKeys(b.Crypto.Suite)
	if err != nil {
		return nil, err
	}
	var material protocol.AES256CBCHMACSHA256Material
	if err := json.Unmarshal(fresh.Material, &material); err != nil {
		return nil, err
	}
	asCBOR, err := protocol.MarshalCBOR(material)
	if err != nil {
		return nil, err
	}
	sealed, err := protocol.EncryptStageKeys(pub, asCBOR)
	if err != nil {
		return nil, fmt.Errorf("encrypt the stage keys: %w", err)
	}
	staging := &models.MinionStaging{
		TempID: uuid.New(), BuildID: b.BuildID, OperationID: b.OperationID, Crypto: *fresh, CreatedAt: s.now(),
	}
	if err := s.stagings.Insert(ctx, staging); err != nil {
		return nil, fmt.Errorf("store the staging: %w", err)
	}
	s.logger.Info("minion staged", zap.String("build_id", b.BuildID.String()),
		zap.String("temp_id", staging.TempID.String()), zap.String("operation_id", b.OperationID.String()))

	item, err := protocol.MarshalCBOR(protocol.StageResponseItem{
		T: protocol.ItemStageResponse, TID: staging.TempID[:], Keys: sealed, Corr: stage.Corr,
	})
	if err != nil {
		return nil, err
	}
	return []cbor.RawMessage{item}, nil
}

// buildKeys are the keys of the build id: a build with a suite core
// implements, or ErrUnopened.
func (s *Service) buildKeys(ctx context.Context, buildID uuid.UUID) (*models.Build, protocol.AES256CBCHMACSHA256Material, error) {
	var keys protocol.AES256CBCHMACSHA256Material
	b, err := s.builds.FindByBuildID(ctx, buildID)
	if err != nil {
		if repository.IsNotFound(err) {
			s.logger.Info("minion message under an unknown id", zap.String("id", buildID.String()))
			return nil, keys, ErrUnopened
		}
		return nil, keys, fmt.Errorf("load build: %w", err)
	}
	if b.Crypto == nil || b.Crypto.Suite != protocol.CryptoAES256CBCHMACSHA256 {
		return nil, keys, ErrUnopened
	}
	if err := json.Unmarshal(b.Crypto.Material, &keys); err != nil {
		return nil, keys, fmt.Errorf("read the build's keys: %w", err)
	}
	return b, keys, nil
}

// errItem is a reply's one err item.
func errItem(code, msg string, item int) ([]cbor.RawMessage, error) {
	data, err := protocol.MarshalCBOR(protocol.ErrItem{T: protocol.ItemErr, Code: code, Msg: msg, Item: &item})
	if err != nil {
		return nil, err
	}
	return []cbor.RawMessage{data}, nil
}
