# Embedded HTML-bundle viewer

The build output of `frontend/src/viewer` lands here and is embedded into the
core binary (`viewer.go`). It is generated, not committed:

    make viewer            # from the repo root
    # or: cd frontend && npm run build:viewer

Expected files: `index.html`, `assets/viewer.js`, `assets/viewer.css`,
`assets/drawing.js`, `assets/simple-icons.js`. Without them the HTML export fails with a message saying
so; every other format is unaffected.
