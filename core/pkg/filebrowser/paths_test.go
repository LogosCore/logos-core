package filebrowser

import (
	"regexp"
	"testing"
)

func TestEntryAt(t *testing.T) {
	for _, tc := range []struct{ path, sep, parent, name, key string }{
		{"/", "/", "", "/", "/"},
		{"/home", "/", "/", "home", "/home"},
		{"/home/user/", "/", "/home", "user", "/home/user"},
		{`C:\`, `\`, "", `C:\`, `c:\`},
		{`C:`, `\`, "", `C:\`, `c:\`},
		{`C:\Users`, `\`, `C:\`, "Users", `c:\users`},
		{`C:\Users\Bob\`, `\`, `C:\Users`, "Bob", `c:\users\bob`},
		{`\\srv\share\`, `\`, "", `\\srv\share\`, `\\srv\share\`},
		{`\\srv\share\docs`, `\`, `\\srv\share\`, "docs", `\\srv\share\docs`},
		// A Windows path typed with /: the same entry as with \.
		{`C:/Users/Bob/`, `\`, `C:\Users`, "Bob", `c:\users\bob`},
		{`c:/`, `\`, "", `c:\`, `c:\`},
	} {
		e := entryAt(tc.path, tc.sep)
		if e.Parent != tc.parent || e.Name != tc.name || e.PathKey != tc.key {
			t.Errorf("%q: parent %q name %q key %q", tc.path, e.Parent, e.Name, e.PathKey)
		}
	}
	got := ancestors(entryAt(`C:\Users\Bob\x.txt`, `\`))
	if len(got) != 3 || got[0].Path != `C:\` || got[2].Path != `C:\Users\Bob` {
		t.Fatalf("ancestors: %+v", got)
	}
	if c := child(entryAt("/", "/"), "etc"); c.Path != "/etc" || c.Parent != "/" {
		t.Fatalf("child of the root: %+v", c)
	}
}

func TestSepAndAbsolute(t *testing.T) {
	if sepOf(`C:\x`) != `\` || sepOf(`\\s\h\x`) != `\` || sepOf("/x") != "/" {
		t.Fatal("sepOf")
	}
	if !absolute("/x") || !absolute(`C:\x`) || !absolute("C:/x") || absolute("x/y") || absolute(`C:x`) {
		t.Fatal("absolute")
	}
	if sepOf("C:/Users") != `\` || entryAt("C:/Users", sepOf("C:/Users")).PathKey != entryAt(`C:\Users`, `\`).PathKey {
		t.Fatal("C:/Users is not C:\\Users")
	}
}

func TestPattern(t *testing.T) {
	for q, names := range map[string][2][]string{
		"id_rsa":  {{"id_rsa", "id_rsa.pub", "my_id_rsa"}, {"rsa"}},
		"*.KDBX":  {{"db.kdbx", ".kdbx"}, {"db.kdbx.bak"}},
		"id_rsa*": {{"id_rsa", "id_rsa.pub"}, {"my_id_rsa"}},
		"a?c":     {{"abc"}, {"ac", "abbc"}},
	} {
		re := regexp.MustCompile(pattern(q))
		for _, n := range names[0] {
			if !re.MatchString(n) {
				t.Errorf("%q should match %q", q, n)
			}
		}
		for _, n := range names[1] {
			if re.MatchString(n) {
				t.Errorf("%q should not match %q", q, n)
			}
		}
	}
}
