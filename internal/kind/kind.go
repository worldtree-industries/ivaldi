// Package kind holds the built-in Kinds: what a binary is, the follow-up
// questions add asks about it, its Manifest settings, and its main Template.
//
// Adding a Kind is local: write internal/templates/kinds/<name>.go.tmpl, a
// file here defining the Kind, and an entry in All.
package kind

import (
	"bytes"
	"fmt"
	"slices"
	"text/template"

	"github.com/worldtree-industries/ivaldi/internal/manifest"
	"github.com/worldtree-industries/ivaldi/internal/prompt"
	"github.com/worldtree-industries/ivaldi/internal/scaffold"
	"github.com/worldtree-industries/ivaldi/internal/templates"
)

// mjolnirModule is the go get query for the framework the Deployable Kinds'
// Templates use.
const mjolnirModule = "github.com/dfryer1193/mjolnir@latest"

// Kind is one built-in Kind.
type Kind struct {
	// Name is the Kind as written in the Manifest and offered by add.
	Name string
	// Ask asks this Kind's follow-up questions about one binary. It may be nil
	// when the Kind has none.
	Ask func(q *Questions, name string) (Answers, error)
	// Modules are module queries (for example "example.com/mod@latest") that
	// add fetches with go get when it adds a binary of this Kind.
	Modules []string
}

// All returns every built-in Kind, in the order add offers them.
func All() []Kind {
	return []Kind{HTTP(), Job(), Tool()}
}

// Names returns the names of every built-in Kind, in the order add offers them.
func Names() []string {
	var names []string
	for _, k := range All() {
		names = append(names, k.Name)
	}
	return names
}

// Lookup returns the built-in Kind called name.
func Lookup(name string) (Kind, bool) {
	all := All()
	i := slices.IndexFunc(all, func(k Kind) bool { return k.Name == name })
	if i < 0 {
		return Kind{}, false
	}
	return all[i], true
}

// Questions is what a Kind's Ask may use. It is shared by every binary in one
// add run, so a Kind can see what earlier binaries in the same run chose.
type Questions struct {
	Prompt *prompt.Prompter
	// Binaries are the binaries already in the Manifest, followed by those
	// planned earlier in this run.
	Binaries []manifest.Binary
	// Exists reports whether rel (slash-relative) is already in the Project
	// or planned by an earlier binary in this run.
	Exists func(rel string) (bool, error)
}

// Answers are what a Kind's Ask decided about one binary.
type Answers struct {
	// Settings are written to the binary's Manifest entry after its kind, in
	// order.
	Settings []manifest.Setting
	// Files are extra files to write alongside the binary's main (for example
	// a Layer it needs).
	Files []scaffold.File
}

// Data is what a Kind's main Template is rendered with.
type Data struct {
	// Name is the binary's name.
	Name string
	// ModulePath is the Project's module path.
	ModulePath string
	// Settings holds the binary's Manifest settings by key, for example
	// {{ .Settings.port }}.
	Settings map[string]any
}

// Main renders the Kind's main Template into cmd/<name>/main.go.
func (k Kind) Main(data Data) (scaffold.File, error) {
	src, err := templates.Kinds.ReadFile("kinds/" + k.Name + ".go.tmpl")
	if err != nil {
		return scaffold.File{}, fmt.Errorf("no Template for Kind %q: %w", k.Name, err)
	}
	tmpl, err := template.New(k.Name).Option("missingkey=error").Parse(string(src))
	if err != nil {
		return scaffold.File{}, err
	}
	var buf bytes.Buffer
	if err = tmpl.Execute(&buf, data); err != nil {
		return scaffold.File{}, fmt.Errorf("rendering the %s Template: %w", k.Name, err)
	}
	return scaffold.File{Path: "cmd/" + data.Name + "/main.go", Content: buf.Bytes()}, nil
}
