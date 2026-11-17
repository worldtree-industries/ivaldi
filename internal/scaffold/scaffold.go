// Package scaffold writes Project files under an explicit Project directory.
// It never reads the process's working directory, stdin or stdout, and never
// runs external processes: the command layer does that through its runner.
package scaffold

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"text/template"

	"github.com/worldtree-industries/ivaldi/internal/templates"
)

const (
	dirPerm  = 0o755
	filePerm = 0o644
)

// File is one file Ivaldi writes into a Project.
type File struct {
	// Path is relative to the Project directory, using forward slashes.
	Path    string
	Content []byte
	// Managed marks a Managed file: identical in every Project, never edited by
	// users, and kept current by a separate system. Any other file is
	// Unmanaged: written once, then owned by the user. At init both kinds are
	// written only if missing.
	Managed bool
}

// Exists reports whether rel exists under the Project directory.
func Exists(dir, rel string) (bool, error) {
	_, err := os.Stat(filepath.Join(dir, rel))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return false, err
}

// The Layers, each a top-level package of the Project.
const (
	LayerAPI     = "api"
	LayerService = "service"
	LayerData    = "data"
	LayerModels  = "models"
	LayerClient  = "client"
)

// rootLayers are the Layers every Project starts with. client is created only
// on request, when the first http binary is added.
//
//nolint:gochecknoglobals // Constant list.
var rootLayers = []string{LayerAPI, LayerService, LayerData, LayerModels}

// SharedMakefilePath is where the Managed shared make file lives.
const SharedMakefilePath = "ivaldi.mk"

// layerRule is one Layer's import rule, enforced by depguard.
type layerRule struct {
	Name   string
	Denied []string // Layers it must not import.
	Rule   string   // Human-readable rule, shown when lint fails.
}

//nolint:gochecknoglobals // Constant table.
var layerRules = []layerRule{
	{
		Name:   LayerAPI,
		Denied: []string{LayerData, LayerClient},
		Rule:   "api may import only service and models",
	},
	{
		Name:   LayerService,
		Denied: []string{LayerAPI, LayerClient},
		Rule:   "service may import only data and models",
	},
	{
		Name:   LayerData,
		Denied: []string{LayerAPI, LayerService, LayerClient},
		Rule:   "data may import only models",
	},
	{
		Name:   LayerModels,
		Denied: []string{LayerAPI, LayerService, LayerData, LayerClient},
		Rule:   "models may not import any layer",
	},
	{
		Name:   LayerClient,
		Denied: []string{LayerAPI, LayerService, LayerData},
		Rule:   "client may import only models",
	},
}

// Skeleton returns the files of a new Project with no binaries.
func Skeleton(modulePath string) ([]File, error) {
	files := []File{
		{Path: "go.mod", Content: fmt.Appendf(nil, "module %s\n\ngo 1.27\n", modulePath)},
		{Path: "Makefile", Content: templates.Makefile},
		{Path: SharedMakefilePath, Content: templates.SharedMakefile, Managed: true},
	}
	lintConfig, err := render("golangci", templates.GolangCI, map[string]any{
		"ModulePath": modulePath,
		"Layers":     layerRules,
	})
	if err != nil {
		return nil, err
	}
	files = append(files, File{Path: ".golangci.yml", Content: lintConfig})

	readme, err := render("readme", templates.Readme, map[string]any{"Title": path.Base(modulePath)})
	if err != nil {
		return nil, err
	}
	files = append(files, File{Path: "README.md", Content: readme})

	for _, layer := range rootLayers {
		doc, docErr := LayerDoc(layer)
		if docErr != nil {
			return nil, docErr
		}
		files = append(files, doc)
	}
	return files, nil
}

// LayerDoc returns the doc file of the named Layer.
func LayerDoc(layer string) (File, error) {
	content, err := templates.Layers.ReadFile("layers/" + layer + ".go.tmpl")
	if err != nil {
		return File{}, fmt.Errorf("no doc file for layer %q: %w", layer, err)
	}
	return File{Path: layer + "/doc.go", Content: content}, nil
}

// Write writes each file that doesn't exist yet and never overwrites one that
// does, reporting each skipped file on out.
func Write(dir string, out io.Writer, files []File) error {
	for _, f := range files {
		if err := writeIfMissing(dir, out, f); err != nil {
			return err
		}
	}
	return nil
}

func render(name, content string, data any) ([]byte, error) {
	tmpl, err := template.New(name).Option("missingkey=error").Parse(content)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err = tmpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeIfMissing(dir string, out io.Writer, f File) error {
	exists, err := Exists(dir, f.Path)
	if err != nil {
		return err
	}
	if exists {
		_, err = fmt.Fprintf(out, "File %s already exists, skipping...\n", f.Path)
		return err
	}

	path := filepath.Join(dir, filepath.FromSlash(f.Path))
	if err = os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return err
	}
	return os.WriteFile(path, f.Content, filePerm)
}
