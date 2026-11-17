package minionfiles

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png" // a screenshot is a PNG or a JPEG
	"io"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.mongodb.org/mongo-driver/bson"
	"go.uber.org/zap"
	"golang.org/x/image/draw"
)

// A screenshot task's picture (docs: contracts/task-results-and-actions.md,
// screenshot) comes as a download's file does, with a name and no path: it
// is on no file system. Core marks it at its first chunk
// (models.MinionFileKindScreenshot), so an operation's screenshots are found
// by their files alone (Files). Once kept, it must be a PNG or a JPEG, and
// core keeps a thumbnail of it beside it (thumbs/<file>): a JPEG fitted
// into ThumbSide pixels a side. The task's data, when its last part comes,
// says the picture's size in pixels and its screen, which core copies onto
// the file (TakeScreenshot).

// ThumbSide is the most a screenshot's thumbnail is a side, in pixels.
const ThumbSide = 400

const (
	// thumbMaxBytes and thumbMaxPixels bound what a thumbnail is made of: the
	// picture is decoded whole, 4 bytes a pixel, so a bigger one gets none.
	thumbMaxBytes  = 64 << 20
	thumbMaxPixels = 40_000_000
	thumbQuality   = 80
	// thumbsAtOnce is how many thumbnails are made at once.
	thumbsAtOnce = 2
)

// screenshotTypes are the pictures a screenshot may be.
var screenshotTypes = map[string]bool{"image/png": true, "image/jpeg": true}

// errUnreadable is a picture that does not decode.
var errUnreadable = errors.New("the picture does not read")

func thumbKey(fileID uuid.UUID) string { return "thumbs/" + fileID.String() }

// sendsFile reports whether a task of result sends core a file it keeps: a
// download's, or a screenshot's picture.
func sendsFile(result string) bool {
	return result == protocol.ResultFileDownload || result == protocol.ResultScreenshot
}

// kindOf is what a file sent for a task of result is: a screenshot's
// picture, or (empty) a file of its host's file system.
func kindOf(result string) string {
	if result == protocol.ResultScreenshot {
		return models.MinionFileKindScreenshot
	}
	return ""
}

// keptScreenshot follows a screenshot's picture once kept: one that is not
// a PNG or a JPEG, or does not decode, is a problem of its task's data; the
// rest get their thumbnail. It returns the file after, nil when unchanged.
func (s *Service) keptScreenshot(ctx context.Context, f models.MinionFile) *models.MinionFile {
	if !screenshotTypes[f.ContentType] {
		s.screenshotProblem(ctx, f, fmt.Sprintf("the picture is %s, not a PNG or a JPEG", f.ContentType))
		return nil
	}
	thumb, err := s.thumbnail(ctx, f)
	switch {
	case errors.Is(err, errUnreadable):
		s.screenshotProblem(ctx, f, err.Error())
		return nil
	case err != nil:
		s.logger.Warn("minion screenshot: no thumbnail", zap.String("file_id", f.FileID.String()), zap.Error(err))
		return nil
	case thumb == nil:
		return nil // too big to make one of
	}
	if err := s.store.Put(ctx, thumbKey(f.FileID), bytes.NewReader(thumb), int64(len(thumb)), "image/jpeg"); err != nil {
		s.logger.Warn("minion screenshot: keep its thumbnail", zap.String("file_id", f.FileID.String()), zap.Error(err))
		return nil
	}
	after, err := s.files.Update(ctx, f.FileID, bson.M{"thumb": true})
	if err != nil {
		s.logger.Error("minion screenshot: record its thumbnail", zap.String("file_id", f.FileID.String()), zap.Error(err))
		return nil
	}
	return after
}

// screenshotProblem records why a screenshot's picture is not one, as a
// problem of its task's data: the task shows its data raw, with why.
func (s *Service) screenshotProblem(ctx context.Context, f models.MinionFile, problem string) {
	s.logger.Warn("minion screenshot: not a picture", zap.String("file_id", f.FileID.String()), zap.String("problem", problem))
	if _, err := s.tasks.SetDataProblem(ctx, f.TaskID, problem); err != nil {
		s.logger.Error("minion screenshot: record the problem", zap.String("task_id", f.TaskID.String()), zap.Error(err))
	}
}

// thumbnail makes a kept picture's thumbnail: nil for one too big to.
func (s *Service) thumbnail(ctx context.Context, f models.MinionFile) ([]byte, error) {
	if f.Total > thumbMaxBytes {
		return nil, nil
	}
	s.thumbs <- struct{}{}
	defer func() { <-s.thumbs }()
	body, _, err := s.store.Get(ctx, objectKey(f.FileID))
	if err != nil {
		return nil, fmt.Errorf("read the picture: %w", err)
	}
	defer body.Close()
	src, err := io.ReadAll(io.LimitReader(body, thumbMaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read the picture: %w", err)
	}
	return makeThumb(src)
}

// makeThumb is a picture's thumbnail: a JPEG fitted into ThumbSide pixels a
// side, on white; nil for a picture of more than thumbMaxPixels.
func makeThumb(src []byte) ([]byte, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(src))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errUnreadable, err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, fmt.Errorf("%w: it has no pixels", errUnreadable)
	}
	if int64(cfg.Width)*int64(cfg.Height) > thumbMaxPixels {
		return nil, nil
	}
	img, _, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errUnreadable, err)
	}
	w, h := fit(cfg.Width, cfg.Height, ThumbSide)
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	draw.BiLinear.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Over, nil)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: thumbQuality}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// fit is a w×h picture's size fitted into side×side, its proportions kept:
// never larger than it was, at least a pixel a side.
func fit(w, h, side int) (int, int) {
	if w <= side && h <= side {
		return w, h
	}
	if w >= h {
		return side, max(1, (h*side+w/2)/w)
	}
	return max(1, (w*side+h/2)/h), side
}

// TakeScreenshot copies a screenshot task's data onto its picture: its
// size in pixels and its screen. It returns why the data is not the picture
// core has ("" when it is): the task's data problem.
func (s *Service) TakeScreenshot(ctx context.Context, taskID uuid.UUID, shot protocol.Screenshot) (string, error) {
	f, err := s.files.FindByTask(ctx, taskID)
	if repository.IsNotFound(err) {
		return "no picture came for it", nil
	}
	if err != nil {
		return "", err
	}
	switch {
	case !bytes.Equal(f.Xfer, shot.Xfer):
		return "the data names another transfer", nil
	case shot.Size != uint64(f.Total):
		return fmt.Sprintf("the data says %d bytes, the picture is %d", shot.Size, f.Total), nil
	case f.SHA256 != nil && !bytes.Equal(f.SHA256, shot.SHA256):
		return "the data's SHA-256 is not the picture's", nil
	}
	set := bson.M{"width": int64(shot.Width), "height": int64(shot.Height)}
	if shot.Display != "" {
		set["display"] = shot.Display
	}
	after, err := s.files.Update(ctx, f.FileID, set)
	if err != nil {
		return "", fmt.Errorf("record the screenshot: %w", err)
	}
	s.publish(after)
	return "", nil
}

// OpenThumb is a screenshot's thumbnail, a JPEG, and its size.
func (s *Service) OpenThumb(ctx context.Context, fileID uuid.UUID) (io.ReadCloser, int64, *models.MinionFile, error) {
	f, err := s.files.FindByID(ctx, fileID)
	if err != nil {
		return nil, 0, nil, err
	}
	if !f.Thumb {
		return nil, 0, f, errors.New("the file has no thumbnail")
	}
	body, info, err := s.store.Get(ctx, thumbKey(f.FileID))
	if err != nil {
		return nil, 0, f, err
	}
	return body, info.ContentLength, f, nil
}

// Files is an operation's files of a kind (a screenshot's), newest first,
// before a page's cursor, at most limit.
func (s *Service) Files(ctx context.Context, operationID uuid.UUID, kind string, before *repository.MinionFileCursor, limit int64) ([]models.MinionFile, error) {
	return s.files.ListByKind(ctx, operationID, kind, before, limit)
}
