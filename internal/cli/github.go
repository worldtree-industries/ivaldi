package cli

import (
	"strings"
)

// allowedActionPatterns are the actions, other than GitHub-owned ones, that a
// Project's CI may run: the shared workflow's org and the third-party actions
// the shared workflow uses.
func allowedActionPatterns() []string {
	return []string{
		"worldtree-industries/*",
		"golangci/golangci-lint-action@*",
		"docker/login-action@*",
		"docker/setup-buildx-action@*",
		"docker/build-push-action@*",
	}
}

// githubSettingsCommands returns the gh commands that apply Ivaldi's repo
// settings to the GitHub repo repo ("owner/name"): only selected actions may
// run, the default GITHUB_TOKEN is read-only, and Actions can't approve pull
// requests.
func githubSettingsCommands(repo string) [][]string {
	base := "repos/" + repo + "/actions/permissions"
	selected := []string{"-F", "github_owned_allowed=true", "-F", "verified_allowed=false"}
	for _, p := range allowedActionPatterns() {
		selected = append(selected, "-f", "patterns_allowed[]="+p)
	}
	return [][]string{
		ghPut(base, "-F", "enabled=true", "-f", "allowed_actions=selected"),
		ghPut(base+"/selected-actions", selected...),
		ghPut(
			base+"/workflow",
			"-f",
			"default_workflow_permissions=read",
			"-F",
			"can_approve_pull_request_reviews=false",
		),
	}
}

// ghPut returns the gh arguments for a PUT to the GitHub API path with fields.
func ghPut(path string, fields ...string) []string {
	return append([]string{"api", "--method", "PUT", path}, fields...)
}

// applyGitHubSettings applies Ivaldi's settings to the Project's GitHub repo
// when origin is on GitHub and gh has admin rights on it. Otherwise, or when a
// call fails, it warns and prints the commands to run by hand. It never fails
// init.
func (s session) applyGitHubSettings() {
	remote, _ := s.output("git", "remote", "get-url", "origin")
	repo, ok := githubRepo(remote)
	if !ok {
		s.warnGitHubSettings("origin is not a GitHub repository", "OWNER/REPO")
		return
	}
	admin, err := s.output("gh", "api", "repos/"+repo, "--jq", ".permissions.admin")
	if err != nil {
		s.warnGitHubSettings(err.Error(), repo)
		return
	}
	if admin != "true" {
		s.warnGitHubSettings("gh has no admin rights on "+repo, repo)
		return
	}
	for _, args := range githubSettingsCommands(repo) {
		if _, err = s.output("gh", args...); err != nil {
			s.warnGitHubSettings(err.Error(), repo)
			return
		}
	}
	s.sayf("Applied GitHub settings to %s.", repo)
}

func (s session) warnGitHubSettings(reason, repo string) {
	s.sayf("Warning: GitHub settings were not applied (%s). To apply them, run:", reason)
	for _, args := range githubSettingsCommands(repo) {
		s.sayf("  %s", shellQuote(append([]string{"gh"}, args...)))
	}
}

// githubRepo returns "owner/name" for a GitHub remote URL.
func githubRepo(remote string) (string, bool) {
	repo, ok := strings.CutPrefix(modulePathFromRemote(remote), "github.com/")
	if !ok || strings.Count(repo, "/") != 1 {
		return "", false
	}
	return repo, true
}

// shellQuote joins args into a line a POSIX shell reads back as args.
func shellQuote(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		if a == "" || strings.ContainsAny(a, " \t\n'\"\\$`*?[]{}()<>|&;#~!") {
			a = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
		quoted[i] = a
	}
	return strings.Join(quoted, " ")
}
