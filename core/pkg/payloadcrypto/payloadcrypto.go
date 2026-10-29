// Package payloadcrypto holds the payload crypto suites core implements: the
// encryption between a minion and core. A factory declares the suite its
// minions speak; core refuses a registration with a suite it does not
// implement, and generates each minion's keys for its factory's suite.
package payloadcrypto

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/logoscore/logos-golang-protocol/protocol"
)

// suites are the suites core implements, each with its key generator.
var suites = map[string]func() (any, error){
	protocol.CryptoAES256CBCHMACSHA256: func() (any, error) {
		m := protocol.AES256CBCHMACSHA256Material{EncKey: make([]byte, 32), MACKey: make([]byte, 32)}
		rand.Read(m.EncKey)
		rand.Read(m.MACKey)
		return m, nil
	},
}

// Supported reports whether core implements suite.
func Supported(suite string) bool {
	_, ok := suites[suite]
	return ok
}

// Suites are the suites core implements, sorted.
func Suites() []string {
	out := make([]string, 0, len(suites))
	for s := range suites {
		out = append(out, s)
	}
	slices.Sort(out)
	return out
}

// NewKeys generates fresh key material for one minion.
func NewKeys(suite string) (*protocol.BuildCrypto, error) {
	gen, ok := suites[suite]
	if !ok {
		return nil, fmt.Errorf("payload crypto suite %q is not implemented", suite)
	}
	m, err := gen()
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return &protocol.BuildCrypto{Suite: suite, Material: raw}, nil
}
