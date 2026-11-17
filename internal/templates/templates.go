package templates

import (
	"embed"
)

// Layers holds each Layer's doc file, as layers/<layer>.go.tmpl.
//
//go:embed layers
var Layers embed.FS

// Makefile is the Unmanaged Makefile, which includes SharedMakefile.
//
//go:embed Makefile
var Makefile []byte

// SharedMakefile is the Managed shared make file, ivaldi.mk.
//
//go:embed ivaldi.mk
var SharedMakefile []byte

// Gitignore is the Unmanaged .gitignore.
//
//go:embed gitignore
var Gitignore []byte

//go:embed ci.tmpl
var CI string

// Readme is the Unmanaged README skeleton template.
//
//go:embed README.md.tmpl
var Readme string

// GolangCI is the Unmanaged lint config template: the golden config plus
// depguard rules for the Layers.
//
//go:embed golangci.yml.tmpl
var GolangCI string

//go:embed main_http.tmpl
var MainHTTP string

// Kinds holds each Kind's main Template, as kinds/<kind>.go.tmpl.
//
//go:embed kinds
var Kinds embed.FS
