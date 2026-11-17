# Projects are public by default

worldtree-industries is on GitHub Free, and on Free three things Ivaldi relies on only work for public repositories. Org-level Actions secrets (`HARBOR_USERNAME`/`HARBOR_PASSWORD`) aren't visible to private repos. The selected-actions `patterns_allowed` list that `init` applies only takes effect on public repos (#15). And standard GitHub-hosted runner minutes are free without limit only on public repos; private ones share the plan's small included allowance. We decided to keep Projects public rather than pay for GitHub Team: the source is public, but images stay private in Harbor, and the shared workflow never logs in or pushes on pull requests.

## Considered Options

- **Upgrade the org to GitHub Team**: fixes org secrets for private repos. It doesn't fix `patterns_allowed`, whose docs aren't plan-specific. Rejected on cost for now.
- **Repo-level secrets on each private Project** (`gh secret set HARBOR_* -R worldtree-industries/<repo>`): works on Free. This is the escape route for the occasional private Project, at the price of a step per repo and a rotation that touches each one.
- **Self-hosted runners in the cluster**, with Harbor credentials from the 1Password operator: no GitHub secrets and no minute limit, but real infrastructure work, and self-hosted runners must never serve public repos. Deferred to the Brokkr era.

## Consequences

Designs that touch CI, secrets or GitHub repository settings should assume a public repo, and check a GitHub feature's plan and visibility limits before relying on it.
