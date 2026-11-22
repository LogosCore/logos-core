package recordings

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"go.mongodb.org/mongo-driver/bson"
	"go.uber.org/zap"
)

// A desktop's recording made a video (docs: Tunnels, Recordings; Core
// Infrastructure, guacenc): once it ended, where core has an encoder, its
// parts go to guacenc-worker, which answers with H.264 in MP4. Its keys
// are read on the way and kept beside the video; then its parts are
// deleted. One at a time, the oldest first.

func videoKey(id uuid.UUID) string { return fmt.Sprintf("video/%s.mp4", id) }
func keysKey(id uuid.UUID) string  { return fmt.Sprintf("keys/%s.json", id) }

// Encoder makes a video of a desktop's recording: guacenc-worker.
type Encoder interface {
	// Encode sends recording, a desktop's of width × height pixels, and
	// returns its video. ErrEncoderAway when the encoder did not take it
	// (unreachable, busy, its own trouble); a *Refusal when it failed on
	// this recording.
	Encode(ctx context.Context, width, height int, recording io.Reader) ([]byte, error)
}

// ErrEncoderAway: the encoder could not be asked, or did not take it: the
// recording waits, and core asks again later.
var ErrEncoderAway = errors.New("the encoder did not answer")

// Refusal is the encoder failing on a recording: what it said.
type Refusal struct{ Msg string }

func (r *Refusal) Error() string { return r.Msg }

// maxVideo bounds a video core takes: a day of a desktop is far less.
const maxVideo = 2 << 30

// httpEncoder asks guacenc-worker over HTTP.
type httpEncoder struct {
	url    string
	client *http.Client
}

// NewHTTPEncoder is guacenc-worker at base (http://guacenc:4823).
func NewHTTPEncoder(base string) Encoder {
	// No timeout: a long recording takes long; the context ends it.
	return &httpEncoder{url: strings.TrimRight(base, "/"), client: &http.Client{}}
}

func (e *httpEncoder) Encode(ctx context.Context, width, height int, recording io.Reader) ([]byte, error) {
	q := url.Values{"width": {strconv.Itoa(width)}, "height": {strconv.Itoa(height)}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.url+"/encode?"+q.Encode(), recording)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEncoderAway, err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusOK:
		video, err := io.ReadAll(io.LimitReader(resp.Body, maxVideo+1))
		if err != nil {
			return nil, fmt.Errorf("%w: its video did not arrive: %v", ErrEncoderAway, err)
		}
		if len(video) > maxVideo {
			return nil, &Refusal{Msg: "the video is past 2 GiB"}
		}
		return video, nil
	case resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnprocessableEntity:
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, &Refusal{Msg: strings.TrimSpace(string(msg))}
	default:
		return nil, fmt.Errorf("%w: %s", ErrEncoderAway, resp.Status)
	}
}

// RecordingKeys are a desktop recording's keys, as Guacamole's player
// reads them: guacd's first sync (Start, the player's 0) and each key
// instruction's keysym, pressed and timestamp, as recorded.
type RecordingKeys struct {
	Start string      `json:"start"`
	Keys  [][3]string `json:"keys"`
}

// readKeys reads a recording's keys as it goes by: to its end, whatever it
// holds (a tail cut short is read as far as it is whole).
func readKeys(r io.Reader) RecordingKeys {
	out := RecordingKeys{Keys: [][3]string{}}
	br := bufio.NewReaderSize(r, 64<<10)
	var ins []string
	for {
		v, end, err := readElement(br)
		if err != nil {
			// Read past what does not parse: the rest still goes to the
			// encoder through the tee, which must not wait on this.
			_, _ = io.Copy(io.Discard, br)
			return out
		}
		ins = append(ins, v)
		if !end {
			continue
		}
		switch {
		case ins[0] == "sync" && len(ins) > 1 && out.Start == "":
			out.Start = ins[1]
		case ins[0] == "key" && len(ins) == 4:
			out.Keys = append(out.Keys, [3]string{ins[1], ins[2], ins[3]})
		}
		ins = ins[:0]
	}
}

// readElement reads one of an instruction's elements: its length (in
// characters), a dot, its value, then a comma (more follow) or a
// semicolon (the instruction's end).
func readElement(br *bufio.Reader) (string, bool, error) {
	n := 0
	for {
		c, err := br.ReadByte()
		if err != nil {
			return "", false, err
		}
		if c == '.' {
			break
		}
		if c < '0' || c > '9' || n > 1<<26 {
			return "", false, fmt.Errorf("not an element's length: %q", c)
		}
		n = n*10 + int(c-'0')
	}
	var b strings.Builder
	for i := 0; i < n; i++ {
		r, _, err := br.ReadRune()
		if err != nil {
			return "", false, err
		}
		b.WriteRune(r)
	}
	c, err := br.ReadByte()
	if err != nil {
		return "", false, err
	}
	switch c {
	case ',':
		return b.String(), false, nil
	case ';':
		return b.String(), true, nil
	}
	return "", false, fmt.Errorf("not an element's end: %q", c)
}

// convertTries is how often the encoder may fail on a recording before it
// is left as it was (FAILED, its parts kept).
const convertTries = 5

// convertLease is how long a conversion holds its recording: a core that
// stops while one converts leaves it to be taken again past it.
const convertLease = 6 * time.Hour

// convertLoop converts recordings while there are any, then looks again
// every ConvertEvery; an encoder away pauses it, the pauses growing.
func (s *Service) convertLoop() {
	defer s.wg.Done()
	away := 0
	for {
		wait := s.cfg.ConvertEvery
		switch err := s.ConvertNext(context.Background()); {
		case err == nil:
			away = 0
			continue
		case errors.Is(err, errNoneToConvert):
			away = 0
		case errors.Is(err, ErrEncoderAway):
			away++
			wait = min(s.cfg.ConvertEvery<<min(away, 6), 30*time.Minute)
			s.logger.Warn("The encoder did not answer: recordings wait", zap.Duration("next_try", wait), zap.Error(err))
		default:
			s.logger.Warn("A recording's video", zap.Error(err))
		}
		select {
		case <-s.stop:
			return
		case <-time.After(wait):
		}
	}
}

// errNoneToConvert: no recording waits for its video.
var errNoneToConvert = errors.New("none to convert")

// ConvertNext converts the oldest recording waiting for its video:
// errNoneToConvert for none; ErrEncoderAway when the encoder did not take
// it (it waits); another error when its store failed (it is tried again
// later). An encoder that failed on it is counted on the recording, which
// is left as it was past convertTries.
func (s *Service) ConvertNext(ctx context.Context) error {
	if s.encoder == nil {
		return errNoneToConvert
	}
	now := s.now()
	rec, err := s.repo.ClaimToConvert(ctx, now, now.Add(convertLease))
	if err != nil {
		return fmt.Errorf("find a recording to convert: %w", err)
	}
	if rec == nil {
		return errNoneToConvert
	}
	log := s.logger.With(zap.String("recording_id", rec.RecordingID.String()))
	err = s.convert(ctx, rec)
	var refusal *Refusal
	switch {
	case err == nil:
		log.Info("A recording is a video now", zap.Int64("bytes", rec.Size), zap.Int64("video_bytes", rec.VideoSize))
		return nil
	case errors.As(err, &refusal):
		tries := rec.VideoTries + 1
		set := bson.M{"video_tries": tries, "video_error": refusal.Msg}
		if tries >= convertTries {
			set["video"] = models.RecordingVideoFailed
			log.Warn("The encoder failed on a recording: it stays as it was", zap.Int("tries", tries), zap.String("why", refusal.Msg))
		} else {
			set["video"] = ""
			set["video_next_at"] = now.Add(time.Duration(tries) * 10 * time.Minute)
			log.Warn("The encoder failed on a recording: tried again later", zap.Int("tries", tries), zap.String("why", refusal.Msg))
		}
		s.release(ctx, rec, set)
		return nil
	default:
		// Away, or its store: as it was, asked again later.
		s.release(ctx, rec, bson.M{"video": "", "video_next_at": now.Add(s.cfg.ConvertEvery)})
		return err
	}
}

// release lets a recording go that did not become a video: set, its lease
// ended.
func (s *Service) release(ctx context.Context, rec *models.Recording, set bson.M) {
	set["video_lease"] = nil
	if err := s.repo.Save(ctx, rec.RecordingID, set); err != nil && !repository.IsNotFound(err) {
		s.logger.Warn("A recording's video: say how it went", zap.String("recording_id", rec.RecordingID.String()), zap.Error(err))
	}
}

// convert makes rec a video: its parts to the encoder, its keys read on
// the way; the video and keys kept, the record says so, then its parts
// are deleted.
func (s *Service) convert(ctx context.Context, rec *models.Recording) error {
	parts := &partsReader{ctx: ctx, store: s.store, id: rec.RecordingID, n: rec.Parts}
	defer func() { _ = parts.Close() }()
	pr, pw := io.Pipe()
	keysCh := make(chan RecordingKeys, 1)
	go func() { keysCh <- readKeys(pr) }()
	video, err := s.encoder.Encode(ctx, rec.Width, rec.Height, io.TeeReader(parts, pw))
	// What the encoder did not read is read for the keys all the same.
	if err == nil {
		_, err = io.Copy(pw, parts)
	}
	_ = pw.CloseWithError(err)
	keys := <-keysCh
	if err != nil {
		return err
	}
	if len(video) == 0 {
		return &Refusal{Msg: "the encoder answered with no video"}
	}
	if err := s.store.Put(ctx, videoKey(rec.RecordingID), bytes.NewReader(video), int64(len(video)), "video/mp4"); err != nil {
		return fmt.Errorf("keep its video: %w", err)
	}
	k, _ := json.Marshal(keys)
	if err := s.store.Put(ctx, keysKey(rec.RecordingID), bytes.NewReader(k), int64(len(k)), "application/json"); err != nil {
		_ = s.store.Delete(ctx, videoKey(rec.RecordingID))
		return fmt.Errorf("keep its keys: %w", err)
	}
	// A recording deleted meanwhile, or taken by a retention cleanup (whose
	// conversion outlived its lease), keeps no video.
	if err := s.repo.SaveKept(ctx, rec.RecordingID, bson.M{"video": models.RecordingVideoConverted, "video_size": int64(len(video)),
		"video_error": "", "video_lease": nil, "video_next_at": nil, "parts": 0}); err != nil {
		_ = s.store.Delete(ctx, videoKey(rec.RecordingID))
		_ = s.store.Delete(ctx, keysKey(rec.RecordingID))
		return fmt.Errorf("say it is a video: %w", err)
	}
	_ = s.dropParts(ctx, *rec)
	rec.Video, rec.VideoSize, rec.Parts = models.RecordingVideoConverted, int64(len(video)), 0
	if t, err := s.tasks.AddRecording(ctx, rec.TaskID, 0, 0); err == nil {
		s.publish(t)
	}
	return nil
}

// ErrNotVideo: the recording is not a video (yet).
var ErrNotVideo = errors.New("it is not a video")

// Video reads a converted recording's video whole (a few MB: what the
// browser's player seeks in).
func (s *Service) Video(ctx context.Context, id uuid.UUID) ([]byte, *models.Recording, error) {
	rec, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if rec.CleanedAt != nil {
		return nil, rec, ErrCleaned
	}
	if rec.Video != models.RecordingVideoConverted {
		return nil, rec, ErrNotVideo
	}
	body, _, err := s.store.Get(ctx, videoKey(id))
	if err != nil {
		return nil, rec, fmt.Errorf("its video: %w", err)
	}
	defer func() { _ = body.Close() }()
	b, err := io.ReadAll(io.LimitReader(body, maxVideo))
	return b, rec, err
}

// Keys reads a converted recording's keys (RecordingKeys, as JSON).
func (s *Service) Keys(ctx context.Context, id uuid.UUID) (io.ReadCloser, *models.Recording, error) {
	rec, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if rec.CleanedAt != nil {
		return nil, rec, ErrCleaned
	}
	if rec.Video != models.RecordingVideoConverted {
		return nil, rec, ErrNotVideo
	}
	body, _, err := s.store.Get(ctx, keysKey(id))
	if err != nil {
		return nil, rec, fmt.Errorf("its keys: %w", err)
	}
	return body, rec, nil
}
