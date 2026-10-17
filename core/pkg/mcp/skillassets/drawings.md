# Drawings

Some wiki pages are an Excalidraw canvas — network maps, attack paths.
Listing rows carry `kind:"drawing"`; create one with `create_wiki_document`
and `kind:"drawing"`. Draw when asked; otherwise write the page.

## Draw with diagram text

`draw_wiki_diagram` takes what the diagram is, one statement per line. No
coordinates, ids or bindings: the server lays it out, draws each node as a
card with its icon, and attaches the arrows.

```
zone internal "Internal — 10.0.0.0/24" network {
  ws142: workstation "WS-0142" "Windows 11"
  dc01: domain-controller "DC01" "10.0.5.10"
}
attacker: attacker "Operator"
attacker -> ws142 "phishing" [exploit]
ws142 -> dc01 "pass-the-hash" [lateral]
dc01 +compromised
```

- `id: kind "title" "detail"` a node; both texts optional
- `… color=red icon=key-round` on any node line; `color=default`, `icon=none` undo
- `id: "new title"` retitle one already drawn
- `id +badge -badge` add or remove badges
- `a -> b "label" [style]` a connection; `<->` both ways, `--` no head
- `[lateral curved head=triangle tail=circle]` route and arrowheads
- `zone id "title" kind { … }` a region around the nodes inside; nests
- `remove id`, `remove a -> b`
- `direction right|down`, `style sketch|clean`, `arrows elbow|curved|straight`, `# comment`

Pick short ids (`dc01`); later edits use them.

**Kinds:** `server` `workstation` `laptop` `mobile` `ot-device` `file-share`
`domain-controller` `user` `admin` `database` `web-app` `mail-server`
`internet` `cloud` `router` `switch` `vpn` `firewall` `edr` `attacker`
`c2-server` `shell` `credential` `hash` `vulnerability` `finding`; otherwise
`box` `ellipse` `diamond` `note`. A `box` with an `icon=` is a card of your own.
**Colours:** `gray` `red` `pink` `grape` `violet` `indigo` `blue` `cyan`
`teal` `green` `lime` `yellow` `orange` `black`, or `#rrggbb`. **Icons:**
lucide names (`lock`, `shield`, `cloud`, `bug`…); an unknown one is refused
with icons that fit.
**Badges** (cards): `compromised` `credentials` `privileged` `flag`
`has-finding` `monitored` `out-of-scope` `target`.
**Styles:** `plain` `traffic` `exploit` `lateral` `creds` `c2`.
**Routes:** `elbow` (default: horizontal and vertical, around other shapes)
`curved` `straight`. **Heads:** `arrow` `triangle` `circle` `diamond` `bar`
`crowfoot-one` `crowfoot-many` `crowfoot-one-or-many` (`-outline` for
hollow), `none`.
**Zones:** `network` `dmz` `cloud` `trust`.

A write merges: what the text names is added or changed, the rest stays,
and nothing already drawn moves. Send only the change — `dc01 +compromised`
is a whole call. Re-sending a node by id updates it in place. A text with
problems draws nothing and lists each by line; fix all and resend.
`mode:"replace"` clears the canvas first. The response is the drawing as it
now reads.

## Read first

`get_wiki_drawing` returns the same language, so you can edit a line and send
it back. Cards placed from the panel and labelled boxes drawn by hand are
nodes under their element ids. Free text, freehand and loose arrows come back
as `otherShapes`; diagram writes leave them alone.

## Exact shapes

`edit_wiki_drawing` takes Excalidraw elements, for geometry or shapes the
language cannot name. Read with `view:"outline"` for ids and positions.
Modes: `add` (default), `update` (only the fields sent; `label` retitles),
`delete` (by `element_ids`), `replace`. Only `type` is required.

```json
{"type":"rectangle","id":"dc","x":100,"y":200,"label":"DC"}
{"type":"arrow","startBinding":"dc","endBinding":"jump","z":-1}
```

A bound arrow without `points` is drawn between its shapes. Higher `z`
covers lower. An update matching no id changes nothing — read again.

Both tools merge with what the operator is doing; `watchers` says whether
anyone was looking.
