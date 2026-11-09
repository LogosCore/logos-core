package resolver

import (
	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// buildServeModels are a delegate command's serves.
func buildServeModels(serves []protocol.ChannelVersions) []*model.BuildServe {
	out := make([]*model.BuildServe, len(serves))
	for i, s := range serves {
		out[i] = &model.BuildServe{ModuleName: s.ModuleName, Versions: append([]int{}, s.Versions...), Kinds: append([]string{}, s.Kinds...)}
	}
	return out
}
