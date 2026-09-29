package web

import (
	"errors"
	"fmt"
	"io/fs"
)

// errFrontendNotEmbedded is reported by builds without embedded panel assets.
var errFrontendNotEmbedded = errors.New("frontend not embedded")

// embeddedFrontendDistFS resolves the "dist" subtree of an embedded frontend
// asset tree. A build whose asset tree carries no usable dist directory reports
// a controlled error here instead of panicking, so callers fail startup with a
// clear record or keep serving API endpoints — never a half-alive panel.
func embeddedFrontendDistFS(assets fs.FS) (fs.FS, error) {
	distFS, err := fs.Sub(assets, "dist")
	if err != nil {
		return nil, fmt.Errorf("embedded frontend assets are unusable: %w", err)
	}
	// fs.Sub only prefixes paths for most FS implementations, so a build whose
	// asset tree carries no dist entry (an embed build without a bundled panel)
	// only fails here. Verify the panel entry file before handing the tree out.
	indexFile, err := distFS.Open("index.html")
	if err != nil {
		return nil, fmt.Errorf("embedded frontend assets are unusable: %w", err)
	}
	_ = indexFile.Close()
	return distFS, nil
}
