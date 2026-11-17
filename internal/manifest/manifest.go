// Package manifest owns the Project's Manifest, .forge.yaml: the record of
// its binaries, each one's Kind, and its per-binary settings.
package manifest

import (
	"github.com/worldtree-industries/ivaldi/internal/scaffold"
)

// FileName is the Manifest's path relative to the Project directory.
const FileName = ".forge.yaml"

// initial is the Manifest of a Project with no binaries yet.
const initial = `# The Ivaldi Manifest: this Project's binaries and their settings.
# ivaldi add appends to it, and you may edit it.
version: 1
binaries: {}
`

// Exists reports whether the Project in dir has a Manifest.
func Exists(dir string) (bool, error) {
	return scaffold.Exists(dir, FileName)
}

// New returns the Manifest file of a Project with no binaries.
func New() scaffold.File {
	return scaffold.File{Path: FileName, Content: []byte(initial)}
}
