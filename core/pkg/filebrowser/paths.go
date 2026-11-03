package filebrowser

import (
	"regexp"
	"strings"

	"github.com/logoscore/logos-core/core/pkg/models"
)

// Paths as hosts write them: "/" and below on Unix; a drive ("C:\") or a
// share ("\\server\share\") and below on Windows, where case does not tell
// paths apart.

var (
	driveRoot = regexp.MustCompile(`^[A-Za-z]:\\$`)
	bareDrive = regexp.MustCompile(`^[A-Za-z]:$`)
	shareRoot = regexp.MustCompile(`^\\\\[^\\]+\\[^\\]+\\$`)
	bareShare = regexp.MustCompile(`^\\\\[^\\]+\\[^\\]+$`)
)

// sepOf is the separator a path is written with: a Windows one has a drive
// or is a share's, and uses \.
func sepOf(path string) string {
	if strings.HasPrefix(path, `\\`) || (len(path) >= 2 && path[1] == ':') {
		return `\`
	}
	return "/"
}

// absolute: a path from a root, not one relative to a minion's directory.
// A Windows one may be written with / (C:/Users), as Windows takes it.
func absolute(path string) bool {
	return strings.HasPrefix(path, "/") || strings.HasPrefix(path, `\\`) ||
		(len(path) >= 3 && path[1] == ':' && (path[2] == '\\' || path[2] == '/'))
}

// isRoot: a path that is a file system's root.
func isRoot(path, sep string) bool {
	if sep == "/" {
		return path == "/"
	}
	return driveRoot.MatchString(path) || shareRoot.MatchString(path)
}

// clean drops a path's trailing separators, but a root's. A Windows path's
// / are \ (C:/Users is C:\Users, as Windows takes it), so a path typed either
// way is the same entry.
func clean(path, sep string) string {
	if sep == `\` {
		path = strings.ReplaceAll(path, "/", `\`)
		if bareDrive.MatchString(path) || bareShare.MatchString(path) {
			return path + `\`
		}
	}
	for len(path) > 1 && strings.HasSuffix(path, sep) && !isRoot(path, sep) {
		path = strings.TrimSuffix(path, sep)
	}
	return path
}

func key(path, sep string) string {
	if sep == `\` {
		return strings.ToLower(path)
	}
	return path
}

// entryAt is the entry a path is: its parent ("" for a root), name and keys.
func entryAt(path, sep string) models.FsEntry {
	path = clean(path, sep)
	e := models.FsEntry{Path: path, PathKey: key(path, sep), Sep: sep}
	if isRoot(path, sep) {
		e.Name = path
	} else {
		i := strings.LastIndex(path, sep)
		parent := clean(path[:max(i, 0)], sep)
		if parent == "" {
			parent = sep
		}
		if sep == `\` && bareDrive.MatchString(path[:max(i, 0)]) {
			parent = path[:i+1]
		}
		e.Parent, e.ParentKey, e.Name = parent, key(parent, sep), path[i+1:]
	}
	e.NameKey = strings.ToLower(e.Name)
	return e
}

// child is the entry of name in folder.
func child(folder models.FsEntry, name string) models.FsEntry {
	p := folder.Path
	if !strings.HasSuffix(p, folder.Sep) {
		p += folder.Sep
	}
	return entryAt(p+name, folder.Sep)
}

// ancestors are the folders on the way to an entry, from its root.
func ancestors(e models.FsEntry) []models.FsEntry {
	var out []models.FsEntry
	for p := e; p.Parent != ""; {
		p = entryAt(p.Parent, e.Sep)
		out = append([]models.FsEntry{p}, out...)
	}
	return out
}

// pattern is the regular expression a machine search matches lowercase
// names with: a glob (with * or ?) whole, other text as a part.
func pattern(query string) string {
	q := strings.ToLower(strings.TrimSpace(query))
	if !strings.ContainsAny(q, "*?") {
		return regexp.QuoteMeta(q)
	}
	var b strings.Builder
	b.WriteString("^")
	for _, r := range q {
		switch r {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	b.WriteString("$")
	return b.String()
}
