# Wiki: reading, editing, templates

## Finding the right page

`search_wiki` returns a `snippet` with every hit — read them before opening a
page. `list_wiki_tree` shows how the notes are organised: two levels by
default, `childCount` per page, `parent_id` to descend, `depth:-1` for all.

## Reading only part of a page

`get_wiki_document` returns a small page's body. A page over 8 KB comes back as
an **outline** (headings, nesting, bytes under each) unless you pass
`full:true`. From the outline:

- `section:"<heading>"` returns that heading and everything nested under it.
- Section sizes include children, so they do not sum to the page.
- Text above the first heading is in **no** section; only `full:true` reaches
  it (the outline reports its size as bytes above the first heading).
- A section is **part** of the page. Change it with `edit_wiki_document`;
  `update_wiki_document` would replace the page with the fragment.
- Section text is exact, so it is the cheapest source of `old_text`.

One read returns at most 40 KB. A longer body or section is cut and
`truncated` is set, with the byte offset to resume from on the last line:
repeat with that `offset:` (still `full:true`, or the same `section:`) until
`truncated` is false. Check the bytes read against the outline's size.

## Changing a page

- `edit_wiki_document` replaces an exact snippet. Almost every edit.
- `add_wiki_section` adds to the end (or start with `position:"start"`) for a
  subject the page lacks; enrich one it has with `edit_wiki_document`.
  `document_ids` adds the same content to up to 25 pages at once; retry only
  the failures the result names.
- `update_wiki_document` replaces the whole body. Read the page first and put
  back every construct you are not changing.

Copy `old_text` verbatim, whitespace and list markers included. It must be
unique; if it repeats, add a line either side rather than `replace_all` (which
is for renaming throughout).

Edits are live; a write's `watchers` count says whether the operator saw it.

## Moving a page

`move_wiki_document` files a page under a different parent, with everything
below it; omit `parent_id` for the top level. Don't rebuild-and-trash instead:
that loses the page's history, attachments and links.

## Deleting a page

`delete_wiki_document` trashes a page (an admin can restore it). One with
children is refused unless you pass `with_children:true`; templates always.

## What a page can contain

Preserve these:

- **Checklist items** drive a coverage bar. `get_checklist_status` lists
  every item's key, prompt and state; `set_checklist_answer` fills one or
  many by key — do not use `edit_wiki_document` on checklists. Multi-line
  answers go in a fenced code block. Write tools report checklist coverage
  when the page has items.
- **References.** Three link kinds: `[host](logos://host/<id>)`,
  `[hash](logos://hash/<id>)`, `[page](logos://doc/<id>)`. A credential is a
  block: fence `logos-credential`, body `{"id":"<uuid>"}`, not in a table
  cell. Any other `logos://` URI is refused.
- **Notices** (`:::info`, `:::success`, `:::warning`, `:::tip`) carry a key.
  Use `set_block_content` to replace a notice's content by key.
- Attachments as `[name bytes](/api/v1/wiki/files/<id>)` alone in a paragraph;
  `attach_text_to_wiki_document` returns the exact line.

## Templates

Check `list_wiki_templates` first. It spans this operation's templates and the
shared ones in Public (marked `shared`). Create from one with
`create_wiki_document` and `template_id`; your `title` wins.

Editing an `isTemplate` page changes every page made from it; `set_wiki_template`
is a team decision — propose it.

## Long content

Prose and evidence — a scan, a config, a log — go in the body whole, in one
call (up to 1 MB), in a fenced code block: it reads inline and stays
searchable. Attach a file only when it is a real file (JSON, CSV, PDF, image)
or would bury the page. See attachments.md.
