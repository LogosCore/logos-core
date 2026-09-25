package gqlctx

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
)

// userFinderFunc adapts a function to UserFinder.
type userFinderFunc func(context.Context, uuid.UUID) (models.User, error)

func (f userFinderFunc) FindByID(ctx context.Context, id uuid.UUID) (models.User, error) {
	return f(ctx, id)
}

func TestLoadUser_ParallelLoadsShareOneFetch(t *testing.T) {
	// The shape gqlgen produces: a page's rows resolve in parallel, so every
	// row asks for the same createdBy before any fetch has come back. A memo
	// that only fills on completion fetches once per row.
	ctx := WithUserMemo(context.Background())
	id := uuid.New()
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	var startOnce sync.Once
	finder := userFinderFunc(func(_ context.Context, id uuid.UUID) (models.User, error) {
		calls.Add(1)
		startOnce.Do(func() { close(started) })
		<-release
		return models.User{UserID: id}, nil
	})

	const rows = 20
	var wg sync.WaitGroup
	errs := make(chan error, rows)
	for range rows {
		wg.Add(1)
		go func() {
			defer wg.Done()
			u, err := LoadUser(ctx, finder, id)
			if err == nil && u.UserID != id {
				err = fmt.Errorf("got user %s, want %s", u.UserID, id)
			}
			errs <- err
		}()
	}
	<-started
	time.Sleep(20 * time.Millisecond) // the other rows arrive while the fetch is in flight
	close(release)
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("LoadUser: %v", err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("%d parallel loads of one id made %d queries, want 1", rows, got)
	}
}

func TestLoadUser_WaitersFailWhenTheFetchPanics(t *testing.T) {
	// gqlgen recovers a panicking resolver, but the loads waiting on that
	// fetch would otherwise wait for a result that never comes — or read the
	// zero User as a success. Either way the page hangs or lies.
	ctx := WithUserMemo(context.Background())
	id := uuid.New()
	started, release := make(chan struct{}), make(chan struct{})
	panicking := userFinderFunc(func(context.Context, uuid.UUID) (models.User, error) {
		close(started)
		<-release
		panic("boom")
	})
	go func() {
		defer func() { _ = recover() }()
		_, _ = LoadUser(ctx, panicking, id)
	}()
	<-started

	// A waiter that arrives late fetches for itself; make that an error too,
	// so the only passing outcome is "returned promptly with an error".
	late := userFinderFunc(func(context.Context, uuid.UUID) (models.User, error) {
		return models.User{}, errors.New("waiter fetched for itself")
	})
	waited := make(chan error, 1)
	go func() {
		_, err := LoadUser(ctx, late, id)
		waited <- err
	}()
	time.Sleep(20 * time.Millisecond)
	close(release)

	select {
	case err := <-waited:
		if err == nil {
			t.Fatal("a load waiting on a panicked fetch returned a zero user with no error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a load waiting on a panicked fetch never returned")
	}
}
