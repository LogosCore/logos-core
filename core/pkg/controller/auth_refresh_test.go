package controller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/auth"
	"github.com/logoscore/logos-core/core/pkg/auth/cookies"
	"go.uber.org/zap"
)

// rotateErrTokenStore fails Rotate with a fixed error and records whether
// the grace shadow was consulted afterwards.
type rotateErrTokenStore struct {
	*memTokenStore
	rotateErr   error
	graceLooked bool
}

func (t *rotateErrTokenStore) Rotate(context.Context, uuid.UUID, string, string, time.Duration) (uuid.UUID, error) {
	return uuid.Nil, t.rotateErr
}

func (t *rotateErrTokenStore) LookupGrace(context.Context, uuid.UUID, string) (*auth.GracePayload, error) {
	t.graceLooked = true
	return nil, auth.ErrTokenInvalid
}

func runRefresh(t *testing.T, store auth.TokenStore) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)

	raw, _, err := auth.MintRefreshToken(uuid.New())
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	ctrl := &authController{
		tokenStore: store,
		log:        zap.NewNop(),
		cfg:        AuthControllerConfig{RefreshGraceTTL: 10 * time.Second, IsDev: true},
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/login/refresh", nil)
	c.Request.AddCookie(&http.Cookie{Name: cookies.RefreshTokenCookie, Value: raw})
	ctrl.Refresh(c)
	return w
}

func clearsRefreshCookie(w *httptest.ResponseRecorder) bool {
	for _, ck := range w.Result().Cookies() {
		if ck.Name == cookies.RefreshTokenCookie && ck.MaxAge < 0 {
			return true
		}
	}
	return false
}

// A refresh token the store no longer knows (e.g. a session from before a
// redeploy) must be a clean 401 that clears the cookie, so the browser
// stops presenting it.
func TestRefresh_UnknownTokenIs401AndClearsCookie(t *testing.T) {
	store := &rotateErrTokenStore{memTokenStore: newMemTokenStore(), rotateErr: auth.ErrTokenInvalid}
	w := runRefresh(t, store)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body %s", w.Code, w.Body)
	}
	if !store.graceLooked {
		t.Error("grace shadow was not consulted for a missing token")
	}
	if !clearsRefreshCookie(w) {
		t.Error("response does not clear the refresh cookie")
	}
}

// A genuine store failure is a 500 and nothing else: no fall-through to the
// grace lookup writing a second response on top of the first.
func TestRefresh_StoreFailureIsSingle500(t *testing.T) {
	store := &rotateErrTokenStore{memTokenStore: newMemTokenStore(), rotateErr: errors.New("redis: connection refused")}
	w := runRefresh(t, store)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body %s", w.Code, w.Body)
	}
	if store.graceLooked {
		t.Error("grace lookup ran after the 500 was already written")
	}
	if clearsRefreshCookie(w) {
		t.Error("a transient store failure must not clear the refresh cookie")
	}
}
