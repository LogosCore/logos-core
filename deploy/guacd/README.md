# logos-guacd

Core's guacd: Apache Guacamole's proxy daemon, which draws the tunnels'
desktop fronts (RDP, VNC, SSH, Telnet) for the web app. It is
guacamole-server **1.6.0**, built by Apache's own Dockerfile from the
release's commit, but **against FreeRDP 3** where the official
`guacamole/guacd:1.6.0` image has FreeRDP 2 (2.11.7), and with **patches**
of its own (`patches/`).

## Why not the official image

With FreeRDP 2, guacd 1.6.0 asserts that no paint is open when the RDP
server resizes the desktop (`guac_rdp_gdi_desktop_resize`, gdi.c:169). A
resize that lands during a paint (a desktop following the web app's size:
full screen and back) fails it, and guacd aborts the whole connection:
`GUAC_ASSERT in guac_rdp_gdi_desktop_resize() failed at gdi.c:169`.
GUACAMOLE-2078 fixed it for FreeRDP 3.8 and later alone (there `gdi_resize`
ends the paint itself), and Apache's Dockerfile builds FreeRDP 2 unless told
otherwise (`FREERDP_VERSION`).

## Which FreeRDP

FreeRDP 3.8 or later, for the resize fix, and before 3.31.0: **3.30.0**.
guacd 1.6.0 starts FreeRDP's GDI (`gdi_init`) in its PreConnect callback,
before FreeRDP creates the context's caches; from FreeRDP 3.31.0 that fails
FreeRDP's own assertion as every RDP connection starts
(`brush_cache_register_callbacks`, brush.c), and the desktop dies before it
opens. GUACAMOLE-2328 moves `gdi_init` to PostConnect, on guacamole-server's
patch branch, in no release yet: once a release has it, move FreeRDP to its
newest 3.x (3.31.0 to 3.33.1 are FreeRDP's fixes this image does without).

## The patches

`patches/*.patch`, applied in their order to the release's source before
it builds (`Dockerfile`: Apache's, but for where it takes the source; each
change marked `logos:`). Each adds a connection param guacd does not have:

- `0001-rdp-workstation-name.patch`: RDP's `workstation-name`, the
  computer name NTLM's login (NLA) gives the server, which its logon
  events show (Security 4624 and 4625, Workstation Name). guacd has none:
  FreeRDP gives the host's name, a container's id. FreeRDP 3 takes one
  (`FreeRDP_SspiClientHostname`, `credssp_auth.c`); FreeRDP 2 would not,
  and the patch sets nothing there. Core sends one with every RDP desktop:
  the setting `workstation_name`, else `client_name`, else the task's own.

The runtime image has the time zones too (`tzdata`, Apache's has none):
guacd gives an RDP session's time zone to FreeRDP as `TZ`, which FreeRDP
reads from `/usr/share/zoneinfo` to tell the server; without them every
desktop's was UTC.

Core refuses a desktop whose params guacd does not ask for (`pkg/guacd`,
`0x0100`), saying which: every RDP desktop sends a workstation name (the
task's own, `DESKTOP-…`, when none is set), so a guacd without this patch
opens no RDP desktop.

## What is pinned

`versions.env`: the guacamole-server commit (its `1.6.0` tag's) and the exact
tag of each library the Dockerfile builds from source: FreeRDP, and the
official 1.6.0 image's libssh2, libtelnet, libvncclient and libwebsockets
(left to themselves, they are each library's newest tag when the image
builds). `LOGOS_REVISION` is our build of them, one up for each change of
the patches or the Dockerfile. The image is tagged
`<guacamole-server>-freerdp<FreeRDP>-logos<revision>`:
`ghcr.io/logoscore/logos-guacd:1.6.0-freerdp3.30.0-logos2`.

- **CI**: `.github/workflows/publish-guacd.yml` builds it (linux/amd64:
  FreeRDP takes too long under emulation for arm64), checks guacd starts and
  links FreeRDP 3, and pushes it, on a change to this folder.
- **Development**: `docker-compose.yml`'s `guacd` builds the same locally
  (`logos-guacd:<tag>`): the first build compiles FreeRDP and the rest, about
  ten minutes. A new tag is a new build: `make services` builds it, as the
  old tag's image is not it.
- **Helm**: `guacd.image` names the published image.

## Moving a version

Change `versions.env`, then `docker-compose.yml`'s guacd build and image tag
and the chart's `guacd.image.tag` the same way: `guacd-pins.test.ts` (in
`frontend/src/vendor/guacamole/`) fails until all agree. A change of
`patches/` or `Dockerfile` moves `LOGOS_REVISION` up the same way; a new
guacamole-server or FreeRDP takes it back to 1. A new guacamole-server
release takes its own Dockerfile again, with the `logos:` changes, and
each patch made again against its source (`git apply --check`). A new
guacamole-server release moves the web app's vendored client too: the same
release, from Maven Central (its header says how), and its checksum in
`guacamole-vendor.test.ts`. Check a desktop resizes in the web app (full
screen and back) before trusting a new FreeRDP.
