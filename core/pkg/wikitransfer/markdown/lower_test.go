package markdown

import (
	"strings"
	"testing"
)

func TestLowerChecklists_BecomesATaskListItem(t *testing.T) {
	body := ":::checklist {\"prompt\":\"Which accounts have a shell?\",\"state\":\"answered\",\"required\":true,\"key\":\"shells\"}\n" +
		"Three, from `/etc/passwd`.\n" +
		":::\n"

	got := LowerChecklists(body)
	want := "- [x] **Which accounts have a shell?**\n" +
		"\n" +
		"  Three, from `/etc/passwd`.\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
	// `key` and `required` are our coverage bookkeeping and say nothing to
	// a reader, so they go with the container.
	if strings.Contains(got, "required") || strings.Contains(got, "shells") {
		t.Errorf("bookkeeping leaked into the markdown:\n%s", got)
	}
}

func TestLowerChecklists_StatesAndHints(t *testing.T) {
	cases := []struct {
		name string
		info string
		want string
	}{
		{
			name: "unanswered is an empty box",
			info: `{"prompt":"Ask this"}`,
			want: "- [ ] **Ask this**",
		},
		{
			name: "flagged is unticked and says so",
			info: `{"prompt":"Ask this","state":"flagged"}`,
			want: "- [ ] **Ask this** _(flagged)_",
		},
		{
			name: "not applicable is settled and says so",
			info: `{"prompt":"Ask this","state":"not_applicable"}`,
			want: "- [x] **Ask this** _(not applicable)_",
		},
		{
			name: "an answered item needs no label — the tick is the label",
			info: `{"prompt":"Ask this","state":"answered"}`,
			want: "- [x] **Ask this**",
		},
		{
			name: "a disabled command hint is not a hint",
			info: `{"prompt":"Ask this","commandHint":"id","commandHintEnabled":false}`,
			want: "- [ ] **Ask this**",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := strings.TrimSpace(LowerChecklists(":::checklist " + c.info + "\n:::\n"))
			if got != c.want {
				t.Errorf("got %q want %q", got, c.want)
			}
		})
	}
}

func TestLowerChecklists_CommandHint(t *testing.T) {
	got := LowerChecklists(":::checklist {\"prompt\":\"Who can log in?\",\"commandHint\":\"getent passwd\",\"commandHintEnabled\":true}\n:::\n")
	want := "- [ ] **Who can log in?**\n\n  `getent passwd`\n"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// An answer is usually command output, which means a fenced block — and
// `:::` inside that block is sample text, not the end of the checklist.
func TestLowerChecklists_KeepsFencedAnswerIntact(t *testing.T) {
	body := ":::checklist {\"prompt\":\"What did the scan say?\",\"state\":\"answered\"}\n" +
		"Output:\n" +
		"\n" +
		"```text\n" +
		":::\n" +
		"not the end\n" +
		"```\n" +
		":::\n"

	got := LowerChecklists(body)
	want := "- [x] **What did the scan say?**\n" +
		"\n" +
		"  Output:\n" +
		"\n" +
		"  ```text\n" +
		"  :::\n" +
		"  not the end\n" +
		"  ```\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// A notice inside an answer closes with its own `:::`, which must not be
// read as the checklist's.
func TestLowerChecklists_HandlesNestedContainer(t *testing.T) {
	body := ":::checklist {\"prompt\":\"Anything odd?\",\"state\":\"flagged\"}\n" +
		":::warning\n" +
		"auditd is installed but not running\n" +
		":::\n" +
		"Raised with the client.\n" +
		":::\n" +
		"\n" +
		"After the list.\n"

	got := LowerChecklists(body)
	want := "- [ ] **Anything odd?** _(flagged)_\n" +
		"\n" +
		"  :::warning\n" +
		"  auditd is installed but not running\n" +
		"  :::\n" +
		"  Raised with the client.\n" +
		"\n" +
		"After the list.\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// Outline's own containers are not ours to lower: a notice is syntax one of
// this zip's target tools understands.
func TestLowerChecklists_LeavesNoticesAlone(t *testing.T) {
	body := ":::warning\nmind the gap\n:::\n\n:::info\nfyi\n:::\n"
	if got := LowerChecklists(body); got != body {
		t.Errorf("notices were rewritten:\n%s", got)
	}
}

// A fenced code block that happens to contain a checklist container is
// somebody documenting the syntax, not using it.
func TestLowerChecklists_IgnoresContainersInsideCode(t *testing.T) {
	body := "```markdown\n:::checklist {\"prompt\":\"x\"}\nanswer\n:::\n```\n"
	if got := LowerChecklists(body); got != body {
		t.Errorf("a code sample was rewritten:\n%s", got)
	}
}

// A container we cannot read the info line of keeps its content, which
// matters more than its shape.
func TestLowerChecklists_LeavesUnparseableInfoAlone(t *testing.T) {
	body := ":::checklist {this is not json}\nanswer\n:::\n"
	if got := LowerChecklists(body); got != body {
		t.Errorf("an unreadable container was rewritten:\n%s", got)
	}
}

func TestLowerChecklists_UnclosedContainerTakesTheRest(t *testing.T) {
	body := ":::checklist {\"prompt\":\"Unfinished\"}\nanswer\n"
	got := LowerChecklists(body)
	want := "- [ ] **Unfinished**\n\n  answer"
	if strings.TrimRight(got, "\n") != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestLowerChecklists_NoChecklistsIsNoChange(t *testing.T) {
	body := "# Title\n\nJust prose with a `:::` in code.\n"
	if got := LowerChecklists(body); got != body {
		t.Errorf("body changed: %q", got)
	}
}
