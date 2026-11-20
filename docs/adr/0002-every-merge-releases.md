# Every merge to main releases

Projects were released by pushing a `vX.Y.Z` tag by hand, which built that version's images. We decided that every merge to `main` releases, as mjolnir does (its ADR-0006): the merged pull request's `semver:minor` or `semver:major` label chooses the bump, and no label means a patch. Unlike mjolnir, the release happens inside the shared workflow's run for the merge, not in a separate workflow. That run tags the affected Deployables' images with the next version, then pushes the git tag, last, through the GitHub API. Tags pushed with `GITHUB_TOKEN` start no workflow runs, so a separate tag-only workflow would leave the release with no images. Pushing the tag last means a failed build leaves no tag behind, and re-running the run retries the release. A run on an already-tagged merge rebuilds that version and tags nothing.

Only merge commits release, found by GitHub's default subject `Merge pull request #N from …`, so `init` allows only merge commits with that message. Like mjolnir, the module path must carry `/vN` for a `vN` release from `v2` up, and every pull request dry-runs its release, so a rejected one fails before the merge.

## Considered Options

- **A separate release workflow that only tags, as in mjolnir.** Rejected: its tag can't trigger the image build without a PAT or GitHub App token, and every Project would need that new secret.
- **Push the git tag first, then build.** Rejected: a failed build would leave a published tag with no images, and a re-run would skip the already-tagged commit.
- **Find the pull request through the API (`commits/{sha}/pulls`) so squash and rebase merges release too.** Rejected for consistency with mjolnir; restricting merge methods is cheap.
- **Drop the `/vN` check because most Projects are Deployables.** Rejected: Tools are `go install`ed by version, which needs the suffix from `v2` up.

## Consequences

- The caller's `go` job grants `contents: write`, `issues: read` and `pull-requests: read`. The top level stays `contents: read`.
- A mislabelled pull request publishes the wrong version, and docs-only merges cut a patch release.
- A customised merge commit subject fails the run; tag that merge by hand.
- The caller's concurrency group keeps only the newest waiting push to `main`, so in a burst of merges one can be released by the next merge's run, at that pull request's bump.
- A release has images only for the Deployables it affected. Deploy each Deployable from the newest version that built it.
