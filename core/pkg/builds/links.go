package builds

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
)

// A build's files and its artifact go through core (docs: minion-build,
// Files): a factory.build carries links to core's own endpoints
// (transfer.go), paths on core's address that the factory follows from
// wherever it reaches core. Each link is signed for one build's file or its
// artifact, until a minute past the build's deadline; the storage behind
// them is never reached by a factory.

// linkPrefix is where the links lead, and the routes that serve them.
const linkPrefix = "/api/factory/builds/"

// FilePath is the path of build's file id: GET.
func FilePath(build, file uuid.UUID) string {
	return linkPrefix + build.String() + "/files/" + file.String()
}

// ArtifactPath is the path build's artifact is uploaded to: PUT.
func ArtifactPath(build uuid.UUID) string {
	return linkPrefix + build.String() + "/artifact"
}

// Link errors: a link that is not core's (or was changed), and one past its
// expiry.
var (
	ErrLinkSignature = errors.New("the link is not signed for this")
	ErrLinkExpired   = errors.New("the link has expired")
)

// Links signs the links a factory.build carries, and checks them when a
// factory follows one. The key is derived from a secret core keeps across
// restarts (JWT_SECRET_KEY), so a build's links outlive a restart of core.
type Links struct {
	key []byte
}

// NewLinks derives the links' key from secret.
func NewLinks(secret string) (*Links, error) {
	if secret == "" {
		return nil, errors.New("builds: the links need a secret")
	}
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte("logos-core build links v1"))
	return &Links{key: m.Sum(nil)}, nil
}

// Sign is path signed until expires: path?expires=<unix seconds>&sig=<hex>.
func (l *Links) Sign(path string, expires time.Time) string {
	exp := strconv.FormatInt(expires.Unix(), 10)
	return path + "?expires=" + exp + "&sig=" + l.mac(path, exp)
}

// Check says whether expires and sig, a link's query, sign path, and the
// link is still good at now.
func (l *Links) Check(path, expires, sig string, now time.Time) error {
	if !hmac.Equal([]byte(sig), []byte(l.mac(path, expires))) {
		return ErrLinkSignature
	}
	exp, err := strconv.ParseInt(expires, 10, 64)
	if err != nil {
		return ErrLinkSignature // never signed so
	}
	if now.Unix() > exp {
		return ErrLinkExpired
	}
	return nil
}

func (l *Links) mac(path, expires string) string {
	m := hmac.New(sha256.New, l.key)
	m.Write([]byte(path))
	m.Write([]byte{0})
	m.Write([]byte(expires))
	return hex.EncodeToString(m.Sum(nil))
}
