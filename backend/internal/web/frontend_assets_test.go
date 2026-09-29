package web

import (
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"
)

// TestEmbeddedFrontendDistFSMissingDistIsControlledError injects a damaged
// asset tree (no dist directory) and asserts the resolver reports the failure
// instead of panicking, which is what the panel startup paths rely on.
func TestEmbeddedFrontendDistFSMissingDistIsControlledError(t *testing.T) {
	damagedAssets := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html></html>")},
	}

	distFS, err := embeddedFrontendDistFS(damagedAssets)
	if err == nil {
		t.Fatal("expected an error when the asset tree has no dist directory, got nil")
	}
	if distFS != nil {
		t.Fatalf("expected a nil asset tree on failure, got %T", distFS)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected the wrapped fs.ErrNotExist, got %v", err)
	}
}

// TestEmbeddedFrontendDistFSResolvesAssets covers the healthy path so the
// failure test above cannot pass by rejecting every asset tree.
func TestEmbeddedFrontendDistFSResolvesAssets(t *testing.T) {
	assets := fstest.MapFS{
		"dist/index.html":    &fstest.MapFile{Data: []byte("<html>panel</html>")},
		"dist/assets/app.js": &fstest.MapFile{Data: []byte("console.log(1)")},
	}

	distFS, err := embeddedFrontendDistFS(assets)
	if err != nil {
		t.Fatalf("unexpected error for a valid asset tree: %v", err)
	}
	content, err := fs.ReadFile(distFS, "index.html")
	if err != nil {
		t.Fatalf("reading index.html from the resolved dist tree: %v", err)
	}
	if string(content) != "<html>panel</html>" {
		t.Fatalf("unexpected index.html content: %q", content)
	}
}
