# Wiki: reading, editing, templates

## Finding the right page

`search_wiki` returns a `snippet` per hit; read them before opening pages.
`list_wiki_tree`: two levels by default, `parent_id` to descend, `depth:-1`
for all.

## Reading only part of a page

`get_wiki_document` returns a small page's body. Over 8 KB it returns an
**outline** unless you pass `full:true`. From the outline:

- `section:"<heading>"` returns that heading and everything nested under it.
- Section sizes include children, so they do not sum to the page.
- Text above the first heading is in **no** section; only `full:true` reaches
  it.
- A section is **part** of the page. Change it with `edit_wiki_document`;
  `update_wiki_document` would replace the page with the fragment.
- Section text is exact: the cheapest source of `old_text`.

One read returns at most 40 KB; repeat with the `offset:` it names until
`truncated` is false. `read_wiki_attachment` pages the same way.

## Changing a page

- `edit_wiki_document` replaces an exact snippet. Almost every edit.
- `add_wiki_section` adds to the end (or start with `position:"start"`) for a
  subject the page lacks. `document_ids` writes up to 25 pages at once; retry
  only the failures the result names.
- `update_wiki_document` replaces the whole body. Read the page first and put
  back every construct you are not changing.

Copy `old_text` verbatim, whitespace included. It must be unique; if it
repeats, add context (`replace_all` is for renaming).

Edits are live; a write's `watchers` count says whether the operator saw it.

## Moving a page

`move_wiki_document` reparents a page with its subtree; omit `parent_id` for
top level. Never rebuild-and-trash: it loses history and attachments.

## Deleting a page

`delete_wiki_document` trashes a page (restorable). Refused with children
unless `with_children:true`; templates always refused.

## What a page can contain

Preserve these:

- **Checklist items** drive a coverage bar. `get_checklist_status` lists
  every item's key, prompt and state; `set_checklist_answer` fills one or
  many by key — never `edit_wiki_document` on checklists. Multi-line
  answers go in a fenced code block.
- **References.** Three link kinds: `[host](logos://host/<id>)`,
  `[hash](logos://hash/<id>)`, `[page](logos://doc/<id>)`. A credential is a
  block: fence `logos-credential`, body `{"id":"<uuid>"}`, not in a table
  cell. Any other `logos://` URI is refused.
- **Notices** (`:::info`, `:::success`, `:::warning`, `:::tip`) carry a key.
  `get_block_status` lists keys; `set_block_content` writes by key.
- Attachments as `[name bytes](/api/v1/wiki/files/<id>)` alone in a paragraph;
  `attach_text_to_wiki_document` returns the exact line.

## Templates

Check `list_wiki_templates` first. It spans this operation's templates and the
shared ones in Public (marked `shared`). Create from one with
`create_wiki_document` and `template_id`; your `title` wins.

Editing an `isTemplate` page changes every page made from it;
`set_wiki_template` is a team decision: propose it.

## Page metadata

Pages carry `page_type`, `tags` and `status`. Set on create or update.

- **page_type** — "Finding", "Recon Notes", "Playbook", etc. Check
  `list_wiki_page_types` first; reuse the operation's vocabulary.
- **tags** — check `list_wiki_tags` first; bulk-set with `set_wiki_tags`.
  `search_wiki` and `list_wiki_tree` filter by them.
- **status** — `draft`, `stable` (default) or `deprecated`. Agents skip
  deprecated pages and flag drafts as unverified.

## Long content

Evidence (a scan, a config, a log) goes in whole, in a fenced code block: it
reads inline and stays searchable. Attach only a real file (JSON, CSV, image)
or one that would bury the page; see attachments.md.

Too big for one call? Stage it; never cut or skip it. From a shell (URL and
token as in attachments.md):
`curl --data-binary @out.txt -H "Authorization: Bearer $TOKEN" $URL/api/v1/mcp/stage`.
Otherwise `stage_text` in chunks, each with the last `bytes` as `offset`. Pass
the `stage_id` to the write tool instead of its content.
