package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
)

// A user's process marks against a real server: set by a name in any case
// and read back by its key, the name kept as it was marked; set again, one
// mark still, its fields as the new mark says (one it no longer has gone,
// not left from the old); an empty mark kept (it hides a built-in one),
// Remove taking it away; another user's are theirs; the sweep takes the
// marks of a user gone alone: clearings stay, whatever the built-in marks.
func TestIntegrationProcessMarks(t *testing.T) {
	db := integrationDB(t)
	repo := NewMinionProcessMarkRepository(db)
	ctx := context.Background()
	ann, bob, gone := uuid.New(), uuid.New(), uuid.New()
	for _, u := range []uuid.UUID{ann, bob} {
		if _, err := db.Collection(userCollection).InsertOne(ctx, bson.M{"user_id": u}); err != nil {
			t.Fatal(err)
		}
	}

	if err := repo.Set(ctx, ann, "Firefox.exe", "red-strong", "", "🦊"); err != nil {
		t.Fatal(err)
	}
	marks, err := repo.ForUser(ctx, ann)
	if m, ok := marks["firefox.exe"]; err != nil || !ok || m.Name != "Firefox.exe" || m.Color != "red-strong" || m.Emoji != "🦊" || m.UserID != ann {
		t.Fatalf("set: %+v %v", marks, err)
	}
	// Again, in another case: the same mark, its emoji now an icon.
	if err := repo.Set(ctx, ann, "firefox.exe", "", "flame", ""); err != nil {
		t.Fatal(err)
	}
	marks, _ = repo.ForUser(ctx, ann)
	if m := marks["firefox.exe"]; len(marks) != 1 || m.Color != "" || m.Emoji != "" || m.Icon != "flame" || m.Name != "firefox.exe" {
		t.Fatalf("set again: %+v", marks)
	}
	if err := repo.Set(ctx, bob, "sshd", "blue-soft", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := repo.Set(ctx, gone, "bash", "green-soft", "", ""); err != nil {
		t.Fatal(err)
	}
	if marks, _ := repo.ForUser(ctx, ann); len(marks) != 1 {
		t.Fatalf("another user's marks among ann's: %+v", marks)
	}
	if err := repo.Set(ctx, ann, "FIREFOX.EXE", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if marks, _ := repo.ForUser(ctx, ann); len(marks) != 1 || !marks["firefox.exe"].Empty() {
		t.Fatalf("cleared, kept: %+v", marks)
	}
	if err := repo.Remove(ctx, ann, "Firefox.exe"); err != nil {
		t.Fatal(err)
	}
	if marks, _ := repo.ForUser(ctx, ann); len(marks) != 0 {
		t.Fatalf("removed: %+v", marks)
	}
	// Bob's clearings: of a name with a built-in mark or not, both stay.
	for _, name := range []string{"System", "sshd"} {
		if err := repo.Set(ctx, bob, name, "", "", ""); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := repo.RemoveOrphans(ctx); err != nil || n != 1 {
		t.Fatalf("orphans: %d %v", n, err)
	}
	if marks, _ := repo.ForUser(ctx, bob); len(marks) != 2 || !marks["system"].Empty() || !marks["sshd"].Empty() {
		t.Fatalf("bob's after the sweep: %+v", marks)
	}
	if marks, _ := repo.ForUser(ctx, gone); len(marks) != 0 {
		t.Fatalf("a user gone's after the sweep: %+v", marks)
	}
}
