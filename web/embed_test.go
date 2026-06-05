//go:build !dev

package web

import (
	"io/fs"
	"testing"
)

func TestStaticFSContainsIndexHTML(t *testing.T) {
	f, err := StaticFS.Open("dist/index.html")
	if err != nil {
		t.Fatalf("Open dist/index.html: %v", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		t.Fatalf("Stat dist/index.html: %v", err)
	}
	if info.IsDir() {
		t.Fatal("dist/index.html is a directory, expected a file")
	}
	if info.Size() == 0 {
		t.Fatal("dist/index.html is empty")
	}
}

func TestStaticFSCanListDist(t *testing.T) {
	entries, err := fs.ReadDir(StaticFS, "dist")
	if err != nil {
		t.Fatalf("ReadDir dist: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("dist directory is empty in embedded FS")
	}
}
