package models

import (
	"regexp"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// MinionSessionMark is how one operator marks a session for themselves: a
// color (SessionMarkColors) or a style (SessionMarkStyles, in the same field:
// a row has one or the other), an icon (a lucide name, or "si:<slug>" for a
// brand) or an emoji, each optional. Private to the operator, kept for them
// in every browser; nobody else sees it.
type MinionSessionMark struct {
	UserID      uuid.UUID `bson:"user_id" json:"user_id"`
	OperationID uuid.UUID `bson:"operation_id" json:"operation_id"`
	SessionID   uuid.UUID `bson:"session_id" json:"session_id"`
	Color       string    `bson:"color,omitempty" json:"color,omitempty"`
	Icon        string    `bson:"icon,omitempty" json:"icon,omitempty"`
	Emoji       string    `bson:"emoji,omitempty" json:"emoji,omitempty"`
	UpdatedAt   time.Time `bson:"updated_at" json:"updated_at"`
}

// SessionMarkHues and SessionMarkStrengths make the mark colors, "<hue>-<strength>":
// 12 hues in 3 strengths, 36 in all. The SPA keeps how each one looks in
// either theme (session-mark-colors.ts).
var (
	SessionMarkHues      = []string{"red", "orange", "amber", "yellow", "lime", "green", "teal", "cyan", "blue", "indigo", "violet", "pink"}
	SessionMarkStrengths = []string{"soft", "medium", "strong"}
	SessionMarkColors    = func() []string {
		var out []string
		for _, h := range SessionMarkHues {
			for _, s := range SessionMarkStrengths {
				out = append(out, h+"-"+s)
			}
		}
		return out
	}()
)

// SessionMarkStyles are the special looks a mark gives a row in place of a
// color: their own background, font and text effects (session-mark-styles.css
// in the SPA). They share the color field, as one choice of the operator's.
var SessionMarkStyles = []string{"haxor", "rainbow", "amber-crt", "synthwave", "glitch", "on-fire", "gold", "ghost"}

var sessionMarkIconPattern = regexp.MustCompile(`^[A-Za-z0-9:_-]{1,64}$`)

// Empty reports whether the mark marks nothing: it is then removed.
func (m MinionSessionMark) Empty() bool { return m.Color == "" && m.Icon == "" && m.Emoji == "" }

// ValidateSessionMark checks a mark's color, icon and emoji (one of the two
// at most: an emoji or an icon).
func ValidateSessionMark(color, icon, emoji string) string {
	switch {
	case color != "" && !slices.Contains(SessionMarkColors, color) && !slices.Contains(SessionMarkStyles, color):
		return "not one of the mark colors or styles"
	case icon != "" && !sessionMarkIconPattern.MatchString(icon):
		return "not an icon name"
	case emoji != "" && (utf8.RuneCountInString(emoji) > 8 || len(emoji) > 32):
		return "not an emoji"
	case icon != "" && emoji != "":
		return "an icon or an emoji, not both"
	}
	return ""
}
