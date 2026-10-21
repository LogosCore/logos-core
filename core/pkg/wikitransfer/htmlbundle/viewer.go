package htmlbundle

import (
	"embed"
	"errors"
	"io/fs"
)

// The viewer is a frontend build (frontend/src/viewer, `npm run
// build:viewer`) written into ./viewer and embedded, so the core image
// carries the one viewer that matches its data format. The directory holds
// only a README until it is built; see viewer/README.md.
//
//go:embed all:viewer
var viewerFS embed.FS

// ViewerFiles are the paths, relative to the bundle root, every build has.
var ViewerFiles = []string{"index.html", "assets/viewer.js", "assets/viewer.css", "assets/drawing.js", "assets/simple-icons.js"}

// ErrViewerNotBuilt is what an HTML export returns from a binary built
// without the viewer.
var ErrViewerNotBuilt = errors.New("the HTML export viewer is not built into this server (run `make viewer` and rebuild)")

// ViewerAssets is where the viewer's files come from: the embedded build in
// production, a fake in tests.
type ViewerAssets interface {
	// Files returns every viewer file keyed by its path in the bundle.
	Files() (map[string][]byte, error)
}

// EmbeddedViewer is the viewer built into this binary.
func EmbeddedViewer() ViewerAssets { return fsViewer{fsys: viewerFS, dir: "viewer"} }

type fsViewer struct {
	fsys fs.FS
	dir  string
}

func (v fsViewer) Files() (map[string][]byte, error) {
	out := map[string][]byte{}
	for _, name := range ViewerFiles {
		b, err := fs.ReadFile(v.fsys, v.dir+"/"+name)
		if err != nil {
			return nil, ErrViewerNotBuilt
		}
		out[name] = b
	}
	return out, nil
}
