package cli

import (
	"errors"
	"fmt"
	"go/version"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/worldtree-industries/ivaldi/internal/scaffold"
)

// askModulePath returns the module path of an existing go.mod, or asks for
// one, defaulting from origin.
func (s session) askModulePath() (string, error) {
	hasGoMod, err := scaffold.Exists(s.env.Dir, "go.mod")
	if err != nil {
		return "", err
	}
	if hasGoMod {
		return readModulePath(s.env.Dir)
	}
	return s.prompt.Required("Module path", modulePathFromRemote(s.origin()))
}

// modulePathFromRemote turns a git remote URL into a module path, for example
// git@github.com:org/repo.git or https://github.com/org/repo into
// github.com/org/repo. It returns "" for a URL it can't make sense of.
func modulePathFromRemote(remote string) string {
	var host, path string
	if strings.Contains(remote, "://") {
		u, err := url.Parse(remote)
		if err != nil {
			return ""
		}
		host, path = u.Hostname(), u.Path
	} else {
		// scp-like syntax: [user@]host:path
		var ok bool
		if host, path, ok = strings.Cut(remote, ":"); !ok {
			return ""
		}
		if _, h, hasUser := strings.Cut(host, "@"); hasUser {
			host = h
		}
	}
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	if host == "" || path == "" {
		return ""
	}
	return host + "/" + path
}

// readModulePath returns the module path declared in the Project's go.mod.
func readModulePath(dir string) (string, error) {
	content, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("reading the module path: %w", err)
	}
	for line := range strings.SplitSeq(string(content), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module"); ok && rest != "" &&
			(rest[0] == ' ' || rest[0] == '\t') {
			if path := strings.Trim(strings.TrimSpace(rest), `"`); path != "" {
				return path, nil
			}
		}
	}
	return "", errors.New("go.mod declares no module path")
}

// minGoVersion is the lowest go line a Project may have: mjolnir needs it.
const minGoVersion = "1.27"

// askGoLine asks whether to raise an existing go.mod's go line when it's
// below minGoVersion, defaulting to yes. It returns whether to raise it.
func (s session) askGoLine() (bool, error) {
	content, err := os.ReadFile(filepath.Join(s.env.Dir, "go.mod"))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	current := goLine(content)
	if current != "" && version.Compare("go"+current, "go"+minGoVersion) >= 0 {
		return false, nil
	}
	if s.prompt.Bool("Raise the go line to "+minGoVersion+"?", true) {
		return true, nil
	}
	if current == "" {
		current = "missing"
	}
	s.sayf("Warning: keeping the go line (%s): mjolnir needs go %s, so http and job binaries won't build "+
		"until you raise it.", current, minGoVersion)
	return false, nil
}

// goLine returns the version on go.mod's go line, or "" if it has none.
func goLine(goMod []byte) string {
	for line := range strings.SplitSeq(string(goMod), "\n") {
		if fields := strings.Fields(line); len(fields) >= 2 && fields[0] == "go" {
			return fields[1]
		}
	}
	return ""
}

// raiseGoLine raises go.mod's go line to minGoVersion.
func (s session) raiseGoLine() {
	s.runAfterWrite("raise the go line", s.command("go", "mod", "edit", "-go="+minGoVersion))
}
