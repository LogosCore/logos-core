package models

import (
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// MinionSessionFolder is a folder of an operation's sessions: a view of the
// Sessions table its operators share. Every session is in the table's All
// (no folder), and in one folder at most (MinionSession.FolderID).
type MinionSessionFolder struct {
	FolderID    uuid.UUID `bson:"folder_id" json:"folder_id"`
	OperationID uuid.UUID `bson:"operation_id" json:"operation_id"`
	Name        string    `bson:"name" json:"name"`
	// Icon (a lucide name) or Emoji, as a wiki page's; Color is the icon's:
	// a swatch of the wiki's icon picker ("oklch(0.65 0.16 245)"), or
	// #rrggbb.
	Icon  string `bson:"icon,omitempty" json:"icon,omitempty"`
	Emoji string `bson:"emoji,omitempty" json:"emoji,omitempty"`
	Color string `bson:"color,omitempty" json:"color,omitempty"`
	// Order places it among the operation's folders, lowest first.
	Order     int       `bson:"order" json:"order"`
	CreatedAt time.Time `bson:"created_at" json:"created_at"`
	CreatedBy uuid.UUID `bson:"created_by" json:"created_by"`
}

// UnfiledFolder, as a list's folder (the nil UUID, no folder's id), is the
// sessions in no folder: the Sessions table's Unfiled.
var UnfiledFolder = uuid.Nil

// The limits of an operation's folders.
const (
	MaxSessionFolderName = 64
	MaxSessionFolders    = 100
)

// sessionFolderColorPattern is a color as the app's icon picker gives it,
// an OKLCH swatch ("oklch(0.65 0.16 245)", the wiki's palette), or
// #rrggbb: a CSS color the app sets on the icon, nothing else.
var sessionFolderColorPattern = regexp.MustCompile(
	`^(#[0-9a-fA-F]{6}|oklch\(\s*` + oklchNumber + `%?\s+` + oklchNumber + `\s+` + oklchNumber + `(deg)?\s*\))$`)

// oklchNumber is one of an OKLCH color's numbers: a digit at least.
const oklchNumber = `(\d{1,3}(\.\d{1,6})?|\.\d{1,6})`

// ValidateSessionFolder checks a folder's name, icon, emoji and color: "" when
// they do, else why not.
func ValidateSessionFolder(name, icon, emoji, color string) string {
	n := utf8.RuneCountInString(strings.TrimSpace(name))
	switch {
	case n == 0:
		return "a folder needs a name"
	case n > MaxSessionFolderName:
		return "a folder's name is at most 64 characters"
	case icon != "" && !sessionMarkIconPattern.MatchString(icon):
		return "not an icon name"
	case emoji != "" && (utf8.RuneCountInString(emoji) > 8 || len(emoji) > 32):
		return "not an emoji"
	case icon != "" && emoji != "":
		return "an icon or an emoji, not both"
	case color != "" && !sessionFolderColorPattern.MatchString(color):
		return "a color is #rrggbb or oklch(L C H)"
	}
	return ""
}

// MinionSessionFolderChoice is the folder an operator last showed of an
// operation (MinionSessionsLayout.Folders); none: All.
type MinionSessionFolderChoice struct {
	OperationID uuid.UUID `bson:"operation_id" json:"operation_id"`
	FolderID    uuid.UUID `bson:"folder_id" json:"folder_id"`
}
