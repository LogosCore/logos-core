package minionfiles

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
)

// A file read while it comes (docs: contracts/minion-messages/file-transfers.md,
// What core keeps): its bytes in order from any place, from its chunks
// (staging/) while they come, from the kept file once joined, so a view of
// it (a picture drawn as it comes, text, its hex or strings) or a download
// need not wait for its end. Nothing in it is checked before the end: what
// is read before is the bytes as the minion sent them.

// livePoll is how often a reader waiting for a file's next bytes looks at
// its record when no notice came: a notice is this core's own (changes),
// and another core's chunk sends it none.
const livePoll = time.Second

// changes wakes the readers waiting on a file when it changes: a chunk
// taken, its end.
type changes struct {
	mu sync.Mutex
	ch map[uuid.UUID]chan struct{}
}

func newChanges() *changes { return &changes{ch: map[uuid.UUID]chan struct{}{}} }

// wait is closed at the file's next change.
func (c *changes) wait(id uuid.UUID) <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch, ok := c.ch[id]
	if !ok {
		ch = make(chan struct{})
		c.ch[id] = ch
	}
	return ch
}

func (c *changes) notify(id uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ch, ok := c.ch[id]; ok {
		close(ch)
		delete(c.ch, id)
	}
}

// ReadFrom is a file's bytes from off, in order: from its chunks while it
// comes, from the kept file once joined. follow: it waits for the bytes
// still to come and ends at the file's end; else it ends where the bytes
// that have come end. A transfer that fails, or is given up, ends it with
// an error, as does ctx.
func (s *Service) ReadFrom(ctx context.Context, fileID uuid.UUID, off int64, follow bool) io.ReadCloser {
	return &liveReader{s: s, ctx: ctx, id: fileID, off: off, follow: follow}
}

type liveReader struct {
	s      *Service
	ctx    context.Context
	id     uuid.UUID
	off    int64
	follow bool
	// cur reads the piece the reader is in (a chunk, or the kept file) up to
	// end; from is a chunk's start at or before off, where the next chunk
	// is looked for (the file's start at first).
	cur  io.ReadCloser
	end  int64
	from int64
}

func (r *liveReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for {
		if r.cur == nil {
			if err := r.open(); err != nil {
				return 0, err
			}
		}
		n, err := r.cur.Read(p[:min(int64(len(p)), r.end-r.off)])
		r.off += int64(n)
		if r.off >= r.end || errors.Is(err, io.EOF) {
			short := r.off < r.end
			r.closeCur()
			if short {
				return n, fmt.Errorf("the file's bytes at %d: %w", r.off, io.ErrUnexpectedEOF)
			}
			err = nil
		}
		if n > 0 || err != nil {
			return n, err
		}
	}
}

func (r *liveReader) Close() error {
	r.closeCur()
	return nil
}

// closeCur closes the piece read: a chunk read to its end is where the next
// one starts.
func (r *liveReader) closeCur() {
	if r.cur == nil {
		return
	}
	_ = r.cur.Close()
	r.cur = nil
	if r.off == r.end {
		r.from = r.end
	}
}

// open opens the piece of the file the reader is at: waiting for it to come,
// when following.
func (r *liveReader) open() error {
	for {
		// Before the record is read: a change after it still wakes the wait.
		wake := r.s.changes.wait(r.id)
		f, err := r.s.files.FindByID(r.ctx, r.id)
		if err != nil {
			return err
		}
		if r.off >= f.Total {
			return io.EOF
		}
		switch f.State {
		case models.MinionFileComplete:
			return r.openKept(f)
		case models.MinionFileReceiving, models.MinionFileJoining:
			if r.off < f.Received {
				err := r.openChunk()
				if err == nil {
					return nil
				}
				// Joined meanwhile, its chunks dropped: the kept file has it.
				if g, gerr := r.s.files.FindByID(r.ctx, r.id); gerr == nil && g.State == models.MinionFileComplete {
					return r.openKept(g)
				}
				return err
			}
			if !r.follow {
				return io.EOF
			}
		default:
			return fmt.Errorf("the file is %s: %s", strings.ToLower(f.State), f.Err)
		}
		select {
		case <-wake:
		case <-time.After(r.s.livePoll):
		case <-r.ctx.Done():
			return r.ctx.Err()
		}
	}
}

func (r *liveReader) openKept(f *models.MinionFile) error {
	body, _, err := r.s.store.Get(r.ctx, objectKey(f.FileID))
	if err != nil {
		return err
	}
	if err := skipTo(body, r.off); err != nil {
		_ = body.Close()
		return err
	}
	r.cur, r.end = body, f.Total
	return nil
}

// openChunk opens the chunk the reader is in, found from the one known to
// start at from: chunks are as long as each message had room for.
func (r *liveReader) openChunk() error {
	for {
		info, err := r.s.store.Head(r.ctx, stagingKey(r.id, r.from))
		if err != nil {
			return err
		}
		if info.ContentLength <= 0 {
			return fmt.Errorf("an empty chunk at %d", r.from)
		}
		if r.off >= r.from+info.ContentLength {
			r.from += info.ContentLength
			continue
		}
		body, _, err := r.s.store.Get(r.ctx, stagingKey(r.id, r.from))
		if err != nil {
			return err
		}
		if err := skipTo(body, r.off-r.from); err != nil {
			_ = body.Close()
			return err
		}
		r.cur, r.end = body, r.from+info.ContentLength
		return nil
	}
}

// skipTo moves a body n bytes in: by Seek where it can.
func skipTo(body io.Reader, n int64) error {
	if n == 0 {
		return nil
	}
	if sk, ok := body.(io.Seeker); ok {
		_, err := sk.Seek(n, io.SeekStart)
		return err
	}
	_, err := io.CopyN(io.Discard, body, n)
	return err
}
