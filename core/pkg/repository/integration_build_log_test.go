package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
)

// TestIntegrationBuildLog pins a build's live log as stored: a chunk sent
// again is kept once, pages come in Seq order from a cursor, AddLog counts
// the bytes only while the instance builds it, TrimStart removes the oldest
// chunks covering at least the bytes asked and says what went, and the log
// goes with its build and its operation.
func TestIntegrationBuildLog(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	builds, logs := NewBuildRepository(db), NewBuildLogRepository(db)
	if err := db.IndexSetupErr(); err != nil {
		t.Fatalf("index setup: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	op := uuid.New()
	b := &models.Build{BuildID: uuid.New(), OperationID: op, ModuleName: "bob",
		State: models.BuildStateQueued, Deadline: now.Add(time.Hour), CreatedAt: now}
	if err := builds.Insert(ctx, b); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := builds.AddLog(ctx, b.BuildID, "bob-1", 10); ok {
		t.Fatal("a queued build took a log chunk")
	}
	if ok, err := builds.MarkBuilding(ctx, b.BuildID, "bob-1", "dev", now.Add(time.Hour), 1<<20, "{}", now); !ok || err != nil {
		t.Fatalf("mark building: %v %v", ok, err)
	}

	chunk := func(seq int64, texts ...string) *models.BuildLogChunk {
		c := &models.BuildLogChunk{BuildID: b.BuildID, OperationID: op, Seq: seq, ReceivedAt: now}
		for _, s := range texts {
			c.Lines = append(c.Lines, models.BuildLogLine{Step: "compile", At: now, Text: s})
			c.Bytes += int64(len(s))
		}
		return c
	}
	var total int64
	for _, c := range []*models.BuildLogChunk{chunk(2, "ccc", "dd"), chunk(1, "aaaa"), chunk(3, "e"), chunk(4, "ffffff")} {
		ok, err := logs.Append(ctx, c)
		if !ok || err != nil {
			t.Fatalf("append %d: %v %v", c.Seq, ok, err)
		}
		if total, ok, err = builds.AddLog(ctx, b.BuildID, "bob-1", c.Bytes); !ok || err != nil {
			t.Fatalf("add log: %v %v", ok, err)
		}
	}
	if total != 16 {
		t.Fatalf("log bytes %d, want 16", total)
	}
	if _, ok, _ := builds.AddLog(ctx, b.BuildID, "bob-2", 1); ok {
		t.Fatal("another instance's chunk counted")
	}
	if ok, err := logs.Append(ctx, chunk(2, "again")); ok || err != nil {
		t.Fatalf("a chunk sent again: %v %v", ok, err)
	}

	page, err := logs.Page(ctx, b.BuildID, 1, 2)
	if err != nil || len(page) != 2 || page[0].Seq != 2 || page[1].Seq != 3 || page[0].Lines[1].Text != "dd" || !page[0].Lines[0].At.Equal(now) {
		t.Fatalf("page after 1: %+v %v", page, err)
	}

	// 5 bytes asked: chunks 1 (4) and 2 (5) go, 9 bytes and 3 lines.
	bytes, lines, err := logs.TrimStart(ctx, b.BuildID, 5)
	if err != nil || bytes != 9 || lines != 3 {
		t.Fatalf("trimmed %d bytes, %d lines: %v", bytes, lines, err)
	}
	if err := builds.TrimmedLog(ctx, b.BuildID, bytes, lines); err != nil {
		t.Fatal(err)
	}
	got, err := builds.FindByBuildID(ctx, b.BuildID)
	if err != nil || !got.LogStreamed || got.LogBytes != 7 || got.LogTrimmed != 3 {
		t.Fatalf("build %+v %v", got, err)
	}
	if page, _ := logs.Page(ctx, b.BuildID, 0, 10); len(page) != 2 || page[0].Seq != 3 {
		t.Fatalf("after the trim: %+v", page)
	}

	if err := logs.DeleteByBuildID(ctx, b.BuildID); err != nil {
		t.Fatal(err)
	}
	if page, _ := logs.Page(ctx, b.BuildID, 0, 10); len(page) != 0 {
		t.Fatal("the log outlived its build")
	}
	_, _ = logs.Append(ctx, chunk(1, "x"))
	if err := logs.DeleteByOperationID(ctx, op); err != nil {
		t.Fatal(err)
	}
	if page, _ := logs.Page(ctx, b.BuildID, 0, 10); len(page) != 0 {
		t.Fatal("the log outlived its operation")
	}
}
