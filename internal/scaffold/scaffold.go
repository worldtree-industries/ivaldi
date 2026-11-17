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
	"path/filepath"
	"text/template"

	"github.com/worldtree-industries/ivaldi/internal/templates"
)

type Binary struct {
	Name string
	Type string // "http", "worker", "cli", "interactive"
}

type ProjectConfig struct {
	ModulePath string
	Binaries   []Binary
	SetupCI    bool
	GoVersion  string
}

const (
	dirPerm  = 0o755
	filePerm = 0o644
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

// CreateDirectories creates the directories each binary needs.
func CreateDirectories(dir string, binaries []Binary) error {
	dirs := []string{"internal"}
	for _, bin := range binaries {
		dirs = append(dirs, filepath.Join("cmd", bin.Name))
	}

	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(dir, d), dirPerm); err != nil {
			return err
		}
	}
	return nil
}

// WriteMainFiles writes each binary's main package, skipping existing files.
func WriteMainFiles(dir string, out io.Writer, config *ProjectConfig) error {
	for _, bin := range config.Binaries {
		var tmplContent string
		switch bin.Type {
		case "http":
			tmplContent = templates.MainHTTP
		case "worker":
			tmplContent = templates.MainWorker
		case "interactive":
			tmplContent = templates.MainInteractive
		default:
			tmplContent = templates.MainCLI
		}

		content, err := generateTemplate("main", tmplContent, map[string]string{
			"ModulePath": config.ModulePath,
			"BinaryName": bin.Name,
		})
		if err != nil {
			return err
		}

		if err = writeIfMissing(dir, out, filepath.Join("cmd", bin.Name, "main.go"), content); err != nil {
			return err
		}
	}
	return nil
}

// WriteTooling writes the Makefile, lint config and, if requested, CI workflow,
// skipping any that already exist.
func WriteTooling(dir string, out io.Writer, config *ProjectConfig) error {
	makefileContent, err := generateTemplate("makefile", templates.Makefile, config)
	if err != nil {
		return err
	}
	if err = writeIfMissing(dir, out, "Makefile", makefileContent); err != nil {
		return err
	}

	if err = writeIfMissing(dir, out, ".golangci.yml", []byte(templates.GolangCI)); err != nil {
		return err
	}

	if !config.SetupCI {
		return nil
	}
	ciContent, err := generateTemplate("ci", templates.CI, config)
	if err != nil {
		return err
	}
	return writeIfMissing(dir, out, filepath.Join(".github", "workflows", "ci.yml"), ciContent)
}

func generateTemplate(name, content string, data any) ([]byte, error) {
	tmpl, err := template.New(name).Parse(content)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err = tmpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeIfMissing(dir string, out io.Writer, rel string, content []byte) error {
	exists, err := Exists(dir, rel)
	if err != nil {
		return err
	}
	if exists {
		_, err = fmt.Fprintf(out, "File %s already exists, skipping...\n", rel)
		return err
	}

	path := filepath.Join(dir, rel)
	if err = os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return err
	}
	return os.WriteFile(path, content, filePerm)
}
