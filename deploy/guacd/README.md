# logos-guacd

Core's guacd: Apache Guacamole's proxy daemon, which draws the tunnels'
desktop fronts (RDP, VNC, SSH, Telnet) for the web app. It is
guacamole-server **1.6.0**, built by Apache's own Dockerfile from the
release's commit, but **against FreeRDP 3** where the official
`guacamole/guacd:1.6.0` image has FreeRDP 2 (2.11.7).

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

## What is pinned

`versions.env`: the guacamole-server commit (its `1.6.0` tag's) and the exact
tag of each library the Dockerfile builds from source: FreeRDP, and the
official 1.6.0 image's libssh2, libtelnet, libvncclient and libwebsockets
(left to themselves, they are each library's newest tag when the image
builds). The image is tagged `<guacamole-server>-freerdp<FreeRDP>`:
`ghcr.io/logoscore/logos-guacd:1.6.0-freerdp3.30.0`.

- **CI**: `.github/workflows/publish-guacd.yml` builds it (linux/amd64:
  FreeRDP takes too long under emulation for arm64), checks guacd starts and
  links FreeRDP 3, and pushes it, on a change to this folder.
- **Development**: `docker-compose.yml`'s `guacd` builds the same locally
  (`logos-guacd:<tag>`): the first build compiles FreeRDP and the rest, about
  ten minutes.
- **Helm**: `guacd.image` names the published image.

## Moving a version

Change `versions.env`, then `docker-compose.yml`'s guacd build and image tag
and the chart's `guacd.image.tag` the same way: `guacd-pins.test.ts` (in
`frontend/src/vendor/guacamole/`) fails until all agree. A new
guacamole-server release moves the web app's vendored client too: the same
release, from Maven Central (its header says how), and its checksum in
`guacamole-vendor.test.ts`. Check a desktop resizes in the web app (full
screen and back) before trusting a new FreeRDP.
