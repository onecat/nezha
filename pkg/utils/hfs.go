package utils

import (
	"embed"
	"io/fs"
	"os"
)

// HybridFS prefers embedded resources and confines custom files to their root.
type HybridFS struct {
	embedFS fs.FS
	localDir string
}

func NewHybridFS(embedded embed.FS, subDir string, localDir string) (*HybridFS, error) {
	subFS, err := fs.Sub(embedded, subDir)
	if err != nil { return nil, err }
	return &HybridFS{embedFS: subFS, localDir: localDir}, nil
}

func (hfs *HybridFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) { return nil, &fs.PathError{Op:"open", Path:name, Err:fs.ErrInvalid} }
	if file, err := hfs.embedFS.Open(name); err == nil {
		return file, nil
	}
	// OpenRoot prevents traversal through symlinks as well as lexical '..'.
	// Open on demand so deployments without a custom directory still work.
	root, err := os.OpenRoot(hfs.localDir)
	if err != nil { return nil, err }
	defer root.Close()
	return root.FS().Open(name)
}
