# Drawings

Some wiki pages are an Excalidraw canvas — network maps, attack paths, flows.
Listing rows carry `kind:"drawing"`; create one with `create_wiki_document`
and `kind:"drawing"`. Draw when asked; otherwise write the page.

## Draw with Mermaid

`draw_wiki_diagram` takes Mermaid — `flowchart`, `sequenceDiagram`,
`stateDiagram-v2`, `mindmap`, `timeline` or `gantt` — and draws it with the
app's cards; nothing already drawn moves.

```
flowchart LR
  subgraph lan["Internal — 10.0.0.0/24"]
    ws1["WS-0142<br/>Windows 11"]:::workstation
    dc01["DC01<br/>10.0.5.10"]:::domain-controller
  end
  op["Operator"]:::attacker
  op e1@-->|phishing| ws1
  ws1 e2@-->|pass-the-hash| dc01
  kms["HSM"]
  kms@{ icon: "key-round" }
  class dc01 compromised
  class e1 exploit
  class e2 lateral,curved
```

A label's first line is the title, after `<br/>` the detail. Classes give a
node its kind, badges and colour (`class kms red,target`), a subgraph its
zone kind, a link (`e1@-->`) its style, route and `head-`/`tail-` arrowhead.
Links: `-->` `---` `<-->` `-.->` (dotted) `==>` (thick) `--o` `--x`.
`%%` lines take what Mermaid cannot say: `%% remove ws1`, `%% dc01
-compromised`, `%% style clean`, `%% arrows curved`.

**Kinds:** `server` `workstation` `laptop` `mobile` `ot-device` `file-share`
`domain-controller` `user` `admin` `database` `web-app` `mail-server`
`internet` `cloud` `router` `switch` `vpn` `firewall` `edr` `attacker`
`c2-server` `shell` `credential` `hash` `vulnerability` `finding`; a plain
`box`, `ellipse`, `diamond`, `note`. A box with an icon is a card.
**Badges:** `compromised` `credentials` `privileged` `flag` `has-finding`
`monitored` `out-of-scope` `target`. **Colours:** `gray` `red` `pink`
`grape` `violet` `indigo` `blue` `cyan` `teal` `green` `lime` `yellow`
`orange` `black`. **Link styles:** `plain` `traffic` `exploit` `lateral`
`creds` `c2`; routes `elbow` (default) `curved` `straight`. **Zones:**
`network` `dmz` `cloud` `trust`. Icons are lucide names; a wrong one is
refused with icons that fit.

States and mindmaps are drawn like flowcharts: classes, ids and merging
apply. Sequences, timelines and gantts are drawn whole; a `title` names one,
and sending the same title redraws it in place. `%% spa: web-app` gives a
sequence participant a card.

A write merges: what it names is added or changed, the rest stays.
Re-sending a node by id updates it. A text with problems draws nothing and
lists each by line. `mode:"replace"` clears the canvas first.

## Read first

`get_wiki_drawing` returns `mermaid` (the graph) and `diagrams` (the rest,
as sent); change lines and send them back. Shapes with no id of their own read as
`n_<element id>`. Free text, freehand and loose arrows come back as
`otherShapes`.

## Exact shapes

`edit_wiki_drawing` takes Excalidraw elements for geometry Mermaid cannot
express. Read with `view:"outline"` for ids and positions. Modes `add`,
`update` (fields sent; `label` retitles or adds one), `delete`, `replace`;
only `type` is required. Both tools merge with what the operator is doing.

`label` goes on a rectangle, ellipse, diamond or arrow. Words for a
connection belong in the arrow's `label`, which sits on the line and moves
with it — not in a text placed near it. A response note names any text that
runs across an arrow, another text or a box's border.
