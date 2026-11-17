# Brokkr notes

Working notes for Brokkr, the GitOps pipeline that will deploy Ivaldi projects. Brokkr doesn't exist yet. These notes capture what Ivaldi's design expects of it, so the contract survives until Brokkr is built. Move this file to the Brokkr repo once that exists.

Terms (Project, Binary, Deployable, Tool, Kind, Manifest) are defined in [`GLOSSARY.md`](../GLOSSARY.md).

## The contract: `.forge.yaml`

Every Ivaldi project has a `.forge.yaml` at its root. It's the Manifest: the source of truth for the project's binaries. Ivaldi adds entries to it, the user may edit it, and Brokkr reads it.

```yaml
version: 1
binaries:
  api:
    kind: http
    port: 8080
    base_image: gcr.io/distroless/static-debian12:nonroot   # optional
  reaper:
    kind: job
    schedule: daily        # hourly|daily|weekly|monthly|none|<cron>
  wolfctl:
    kind: tool
```

- `binaries` maps each binary name (the directory under `cmd/`) to its settings.
- `kind` is one of `http`, `job` or `tool`.
- `port` (`http` only) is unique within the project.
- `schedule` (`job` only) is a keyword, `none`, or a standard cron expression.
- `base_image` (optional) is the image ko builds on top of. If it's absent, the default is `gcr.io/distroless/static-debian12:nonroot`.
- `build: dockerfile` (optional) means the binary is built from an unmanaged `cmd/<name>/Dockerfile` instead of by ko.

## What Brokkr is expected to do

- **Ignore Tools.** Entries with `kind: tool` are never deployed.
- **Find images:**
  - Each Deployable's image is at `registry.werewolves.fyi/library/<binary>`.
  - Project releases are git tags `vX.Y.Z`. CI only builds Deployables affected since the previous tag, so an unchanged Deployable keeps its older semver tag. Brokkr should track the **latest semver tag per image**, not assume every image has the newest project tag.
  - Pushes to `main` also publish `sha-<short>` tags.
- **`http`:** deploy as a long-running service. Set the `PORT` environment variable from `port`.
- **`job` schedules:**
  - `none`: a one-shot Job (runs once per rollout).
  - `hourly`, `daily`, `weekly`, `monthly`: expand into a cron expression with **stable jitter**, derived from a hash of the project and binary name. It must be deterministic so the schedule doesn't move between renders.
  - A raw cron expression is used as-is.
- **Templating:** fill in a templated deploy manifest per Deployable from its `.forge.yaml` entry.

## Open questions for Brokkr

- How Brokkr finds projects to deploy (repo list, org scan, webhook).
- Which environments exist and how promotion between them works.
- The exact jitter algorithm and the hash inputs.
- Whether `.forge.yaml` declares database needs that Brokkr provisions (CloudNativePG), and how migrations run. Tracked in worldtree-industries/ivaldi#1.
- Probe wiring. mjolnir's router serves `/livez`, `/readyz` and `/startupz`, so Brokkr's `http` template should point the liveness, readiness and startup probes at those. The termination grace period must exceed the app's drain budget. Freshly generated `http` binaries report not-ready until their readiness check is implemented.

## Boundary with the build side

Brokkr is the **deploy** side. Building is not Brokkr's job: lint, test, and building and pushing images happen in each project's CI, through a shared reusable workflow in a dedicated workflows repo. The handoff between the two sides is:

- the images in Harbor (`registry.werewolves.fyi/library/<binary>`, semver and `sha-<short>` tags), and
- `.forge.yaml` in the project repo.
