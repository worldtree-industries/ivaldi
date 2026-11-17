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

// PreCommit is the Managed pre-commit hook, .githooks/pre-commit.
//
//go:embed pre-commit
var PreCommit []byte

// Gitignore is the Unmanaged .gitignore.
//
//go:embed gitignore
var Gitignore []byte

// CI is the Managed CI caller workflow. Its pin of the shared workflow is
// filled in from scaffold.SharedWorkflowSHA and scaffold.SharedWorkflowVersion
// in place of __SHARED_WORKFLOW_SHA__ and __SHARED_WORKFLOW_VERSION__.
//
//go:embed ci.yml
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

// Kinds holds each Kind's main Template, as kinds/<kind>.go.tmpl.
//
//go:embed kinds
var Kinds embed.FS
