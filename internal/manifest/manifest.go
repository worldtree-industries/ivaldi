// Package manifest owns the Project's Manifest, .forge.yaml: the record of
// its binaries, each one's Kind, and its per-binary settings.
//
// Ivaldi only appends to the Manifest. It edits the YAML node tree rather than
// re-marshalling a struct, so the user's entries, comments and ordering survive.
package manifest

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/worldtree-industries/ivaldi/internal/scaffold"
)

// FileName is the Manifest's path relative to the Project directory.
const FileName = ".forge.yaml"

const (
	keyBinaries = "binaries"
	keyKind     = "kind"
	filePerm    = 0o644
	indent      = 2
)

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

// The setting keys Ivaldi writes to a binary's entry. Kind Templates read
// them by the same names, for example {{ .Settings.port }}.
const (
	// SettingPort is an http binary's port.
	SettingPort = "port"
	// SettingSchedule is a job binary's schedule.
	SettingSchedule = "schedule"
)

// Binary is one binary's entry in the Manifest.
type Binary struct {
	Name string
	Kind string
	// Settings holds every other key of the entry, as written (for example
	// "port": "8080").
	Settings map[string]string
}

// Setting is one key of a binary's entry, after its kind.
type Setting struct {
	Key   string
	Value any
}

// Manifest is a parsed Manifest that can be appended to.
type Manifest struct {
	doc yaml.Node
}

// Load reads the Manifest of the Project in dir.
func Load(dir string) (*Manifest, error) {
	content, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		return nil, err
	}
	m, err := Parse(content)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", FileName, err)
	}
	return m, nil
}

// Empty returns the Manifest of a Project with no binaries.
func Empty() *Manifest {
	m, err := Parse([]byte(initial))
	if err != nil {
		panic(err) // The initial Manifest is a constant.
	}
	return m
}

// Parse parses a Manifest's content.
func Parse(content []byte) (*Manifest, error) {
	var m Manifest
	if err := yaml.Unmarshal(content, &m.doc); err != nil {
		return nil, err
	}
	if m.doc.Kind == 0 { // An empty file.
		m.doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
	}
	if m.doc.Kind != yaml.DocumentNode || m.root().Kind != yaml.MappingNode {
		return nil, errors.New("expected a mapping at the top level")
	}
	if b := m.binaries(); b != nil && b.Kind != yaml.MappingNode && !isNull(b) {
		return nil, fmt.Errorf("line %d: expected %s to be a mapping of binary names to entries", b.Line, keyBinaries)
	}
	return &m, nil
}

// Binaries returns the binaries in the Manifest, in order.
func (m *Manifest) Binaries() []Binary {
	b := m.binaries()
	if b == nil || b.Kind != yaml.MappingNode {
		return nil
	}
	var binaries []Binary
	for i := 0; i+1 < len(b.Content); i += 2 {
		bin := Binary{Name: b.Content[i].Value, Settings: map[string]string{}}
		entry := b.Content[i+1]
		if entry.Kind == yaml.MappingNode {
			for j := 0; j+1 < len(entry.Content); j += 2 {
				key, value := entry.Content[j].Value, entry.Content[j+1].Value
				if key == keyKind {
					bin.Kind = value
				} else {
					bin.Settings[key] = value
				}
			}
		}
		binaries = append(binaries, bin)
	}
	return binaries
}

// Add appends a binary's entry: its kind, then settings in order.
func (m *Manifest) Add(name, kind string, settings []Setting) error {
	entry := &yaml.Node{Kind: yaml.MappingNode}
	entry.Content = append(entry.Content, scalar(keyKind), scalar(kind))
	for _, s := range settings {
		var value yaml.Node
		if err := value.Encode(s.Value); err != nil {
			return fmt.Errorf("binary %s: setting %s: %w", name, s.Key, err)
		}
		entry.Content = append(entry.Content, scalar(s.Key), &value)
	}

	b := m.binaries()
	if b == nil {
		b = &yaml.Node{Kind: yaml.MappingNode}
		m.root().Content = append(m.root().Content, scalar(keyBinaries), b)
	} else if isNull(b) {
		*b = yaml.Node{Kind: yaml.MappingNode, HeadComment: b.HeadComment, LineComment: b.LineComment}
	}
	// An empty mapping is usually written in flow style ({}). Switch to block
	// style so entries are written one per line.
	if len(b.Content) == 0 {
		b.Style = 0
	}
	b.Content = append(b.Content, scalar(name), entry)
	return nil
}

// Bytes returns the Manifest's content.
func (m *Manifest) Bytes() ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(indent)
	if err := enc.Encode(&m.doc); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// File returns the Manifest as a file to write.
func (m *Manifest) File() (scaffold.File, error) {
	content, err := m.Bytes()
	if err != nil {
		return scaffold.File{}, err
	}
	return scaffold.File{Path: FileName, Content: content}, nil
}

// Save overwrites the Manifest of the Project in dir.
func (m *Manifest) Save(dir string) error {
	content, err := m.Bytes()
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, FileName), content, filePerm)
}

func (m *Manifest) root() *yaml.Node {
	return m.doc.Content[0]
}

func (m *Manifest) binaries() *yaml.Node {
	root := m.root()
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == keyBinaries {
			return root.Content[i+1]
		}
	}
	return nil
}

func isNull(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && n.Tag == "!!null"
}

func scalar(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}
