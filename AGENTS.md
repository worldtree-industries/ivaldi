# Agent instructions

## Agent skills

### Issue tracker

Issues are tracked in GitHub Issues on worldtree-industries/ivaldi via the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

Default labels: needs-triage, needs-info, ready-for-agent, ready-for-human, wontfix. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: one `GLOSSARY.md` and `docs/adr/` at the repo root. See `docs/agents/domain.md`.

## Pointers

- **Adding a Kind, or testing at the command seam** (scripted stdin, fake runner, mjolnir's API): see `docs/extending-ivaldi.md`.
- **CI, secrets or GitHub settings**: Projects are public by default because of GitHub Free limits. See `docs/adr/0001-projects-are-public-by-default.md`.
