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
	"strings"
	"text/template"

	"github.com/worldtree-industries/ivaldi/internal/templates"
)

const (
	dirPerm  = 0o755
	filePerm = 0o644
	execPerm = 0o755
)

// File is one file Ivaldi writes into a Project.
type File struct {
	// Path is relative to the Project directory, using forward slashes.
	Path    string
	Content []byte
	// Managed marks a Managed file: identical in every Project, never edited by
	// users, and kept current by a separate system. Any other file is
	// Unmanaged: written once, then owned by the user. At init both kinds are
	// written only if missing, unless the user agrees to overwrite them.
	Managed bool
	// Mode is the file's permissions, such as 0o755 for a script. Zero means
	// 0o644.
	Mode fs.FileMode
	// Overwrite writes the file even if it exists: only for a file the user
	// agreed to overwrite.
	Overwrite bool
	// IfExists is what Adoption does when the file already exists.
	IfExists IfExists
	// KeptWarning says what keeping the existing file means, for a file
	// Adoption asks about.
	KeptWarning string
}

// IfExists is what Adoption does with a file that already exists.
type IfExists int

const (
	// AskToOverwrite asks whether to overwrite it, default no, and warns with
	// the File's KeptWarning when it's kept.
	AskToOverwrite IfExists = iota
	// Keep keeps it without asking.
	Keep
	// AppendMissingLines appends the lines of the File's content that it
	// lacks, without asking.
	AppendMissingLines
)

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

// The paths of the Project's root files.
const (
	GoModPath      = "go.mod"
	MakefilePath   = "Makefile"
	LintConfigPath = ".golangci.yml"
	ReadmePath     = "README.md"
	GitignorePath  = ".gitignore"
	// SharedMakefilePath is where the Managed shared make file lives.
	SharedMakefilePath = "ivaldi.mk"
)

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

// The release of worldtree-industries/workflows that the Managed CI workflow
// calls. The workflow is pinned by SharedWorkflowSHA, so a moved tag can't
// change what runs; SharedWorkflowVersion is only the comment beside it.
// Update both together, from:
//
//	gh api repos/worldtree-industries/workflows/commits/<version> --jq .sha
const (
	SharedWorkflowSHA     = "1871647922e0739445221c34f8917ab1566e46b1"
	SharedWorkflowVersion = "v1.2.0"
)

// CIPath is where the Managed CI caller workflow lives.
const CIPath = ".github/workflows/ci.yml"

// HooksDir is the Project's git hooks directory, which init points
// core.hooksPath at. PreCommitHookPath is the Managed pre-commit hook in it.
const (
	HooksDir          = ".githooks"
	PreCommitHookPath = HooksDir + "/pre-commit"
)

// Skeleton returns the files of a new Project with no binaries.
func Skeleton(modulePath string) ([]File, error) {
	files := []File{
		// Its module path is used, so it's always kept.
		{Path: GoModPath, Content: fmt.Appendf(nil, "module %s\n\ngo 1.27\n", modulePath), IfExists: Keep},
		{
			Path: MakefilePath, Content: templates.Makefile,
			KeptWarning: "add include " + SharedMakefilePath + " to it yourself",
		},
		{
			Path: SharedMakefilePath, Content: templates.SharedMakefile, Managed: true,
			KeptWarning: "the shared make targets may be missing or out of date",
		},
		{Path: CIPath, Content: ciWorkflow(), Managed: true, KeptWarning: "no shared CI"},
		{
			Path: PreCommitHookPath, Content: templates.PreCommit, Managed: true, Mode: execPerm,
			KeptWarning: "no pre-commit hook, and core.hooksPath isn't set",
		},
		{Path: GitignorePath, Content: templates.Gitignore, IfExists: AppendMissingLines},
	}
	lintConfig, err := render("golangci", templates.GolangCI, map[string]any{
		"ModulePath": modulePath,
		"Layers":     layerRules,
	})
	if err != nil {
		return nil, err
	}
	files = append(
		files,
		File{Path: LintConfigPath, Content: lintConfig, KeptWarning: "the Layer rules aren't enforced"},
	)

	readme, err := render("readme", templates.Readme, map[string]any{"Title": path.Base(modulePath)})
	if err != nil {
		return nil, err
	}
	files = append(files, File{Path: ReadmePath, Content: readme, IfExists: Keep})

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
	return File{Path: layer + "/doc.go", Content: content, IfExists: Keep}, nil
}

// Write writes each file that doesn't exist yet, reporting each skipped file
// on out. It overwrites an existing file only when the file says Overwrite.
func Write(dir string, out io.Writer, files []File) error {
	for _, f := range files {
		if err := writeIfMissing(dir, out, f); err != nil {
			return err
		}
	}
	return nil
}

func ciWorkflow() []byte {
	return []byte(strings.NewReplacer(
		"__SHARED_WORKFLOW_SHA__", SharedWorkflowSHA,
		"__SHARED_WORKFLOW_VERSION__", SharedWorkflowVersion,
	).Replace(templates.CI))
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
	switch {
	case exists && !f.Overwrite:
		_, err = fmt.Fprintf(out, "File %s already exists, skipping...\n", f.Path)
		return err
	case exists:
		if _, err = fmt.Fprintf(out, "Overwriting %s...\n", f.Path); err != nil {
			return err
		}
	}

	path := filepath.Join(dir, filepath.FromSlash(f.Path))
	if err = os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return err
	}
	perm := f.Mode
	if perm == 0 {
		perm = filePerm
	}
	if err = os.WriteFile(path, f.Content, perm); err != nil {
		return err
	}
	// WriteFile keeps an existing file's mode.
	return os.Chmod(path, perm)
}
