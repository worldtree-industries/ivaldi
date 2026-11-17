package cli

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/worldtree-industries/ivaldi/internal/prompt"
	"github.com/worldtree-industries/ivaldi/internal/scaffold"
)

// askModulePath returns the module path of an existing go.mod, or asks for
// one, defaulting from origin.
func (s session) askModulePath(p *prompt.Prompter) (string, error) {
	hasGoMod, err := scaffold.Exists(s.env.Dir, "go.mod")
	if err != nil {
		return "", err
	}
	if hasGoMod {
		return readModulePath(s.env.Dir)
	}
	return p.Required("Module path", modulePathFromRemote(s.origin()))
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
