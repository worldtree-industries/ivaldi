# Ivaldi

Ivaldi sets up and grows opinionated Go Projects. `ivaldi init` creates a Project's skeleton, and `ivaldi add` adds binaries to it over time.

Ivaldi is stateless. It has no config file and no flags, and it keeps nothing outside the Project. Everything it needs comes from the Project directory and your answers to its prompts.

## Installation

```bash
go install github.com/worldtree-industries/ivaldi/cmd/ivaldi@latest
```

Make sure `$(go env GOPATH)/bin` is on your `PATH`.

## Commands

Both commands work on the current directory, take no arguments and ask everything interactively. Neither commits anything: review the files, then commit them yourself.

### `ivaldi init`

Sets up a new Project in the current directory:

1. Refuses to run if `.forge.yaml` already exists (use `ivaldi add` instead).
2. Asks for the module path, unless there's already a `go.mod`, whose module path it uses. The default comes from the `origin` remote (`git@github.com:org/repo.git` becomes `github.com/org/repo`). With no remote there's no default.
3. Asks for binaries, exactly as `ivaldi add` does. A blank answer creates a Project with no binaries yet.
4. Runs `git init` unless the directory is already the top level of a git repository. A directory inside another repository (a subdirectory of a monorepo, say) gets its own repository, and the outer repository's `origin` is never used for the module path or GitHub settings.
5. Writes the Project files, skipping any file that already exists. It never overwrites anything, so you can run it in a partly set-up repository.
6. Turns on the pre-commit hook with `git config core.hooksPath .githooks`, in the Project's own repository only (never a repository it's inside). If that fails, `init` still succeeds: it warns and prints the command to run later.
7. Applies GitHub repository settings (see [GitHub settings](#github-settings)).

### `ivaldi add`

Adds binaries to an existing Project. It refuses to run without a `.forge.yaml` (run `ivaldi init` first).

1. Asks for a comma-separated list of binary names, for example `web, nightly, cli`.
2. Checks every name before writing anything. A name must be lowercase, start with a letter, use only letters, digits and hyphens, and be at most 63 characters long. It must not appear twice in the list, already be in the Manifest, or already have a `cmd/<name>/` directory. If any name fails, nothing is written and the error names the bad entry.
3. Asks for each binary's Kind, plus that Kind's questions (below). Invalid answers are asked again.
4. Writes each binary's `main` package to `cmd/<name>/main.go` and adds the binary to the Manifest.
5. Fetches the latest mjolnir when a new binary needs it, and runs `go mod tidy`. If either fails (for example, offline), `add` still succeeds: the files are written, and it warns and prints the commands to run later. `init` does the same when you add binaries during `init`.

To remove or rename a binary, delete its `cmd/<name>/` directory and its Manifest entry by hand.

## Kinds

A binary's Kind decides its Template, and whether it's a Deployable (packaged into a container image and deployed) or a Tool (installed on developers' machines). The Kinds are built into Ivaldi.

| Kind   | Is a       | Asks for                                                    |
|--------|------------|-------------------------------------------------------------|
| `http` | Deployable | a port, and (for the first `http` binary) a `client` Layer  |
| `job`  | Deployable | a schedule                                                  |
| `tool` | Tool       | nothing                                                     |

- **`http`** is a long-running HTTP server built on [mjolnir](https://github.com/dfryer1193/mjolnir). It has mjolnir's router with built-in liveness, readiness and startup probes, safe server defaults, a graceful drain on SIGTERM and mjolnir's logger. It listens on `PORT`, falling back to the port chosen during `add`. That port defaults to the next free one from 8080 in the Manifest, must be from 1024 to 65535, and must be unique within the Project. The generated readiness check reports not-ready until you implement it, so it can't be forgotten.
- **`job`** runs once to completion. It calls a single `run(ctx) error`, with a context that's cancelled on SIGINT or SIGTERM, logs through mjolnir's logger and exits with code 1 on error. Its schedule is `hourly`, `daily`, `weekly`, `monthly`, `none` (the default: run once) or a 5-field cron expression. Keywords stay keywords in the Manifest.
- **`tool`** uses the standard `flag` package and plain terminal output, and supports `-version` from the build info. Tools are distributed only with `go install <module>/cmd/<name>@<version>`.

## Layers

`init` creates four Layers as top-level packages, each with a doc comment explaining its role. There's no `internal` directory.

| Layer     | Role                                                    | May import          |
|-----------|---------------------------------------------------------|---------------------|
| `api`     | Inbound requests: transport, routing, request shapes    | `service`, `models` |
| `service` | Business logic                                          | `data`, `models`    |
| `data`    | Persistent state and external stores                    | `models`            |
| `models`  | Domain types                                            | no Layer            |
| `client`  | How other Projects call this one (optional)             | `models`            |

`cmd/*` may import any Layer: each binary's `main` is where the Layers are wired together. The `client` Layer is created only if you say yes when adding the Project's first `http` binary. Lint enforces these rules with depguard. Other Projects should import only `models` and `client`, but that's by convention and isn't enforced.

## The Manifest: `.forge.yaml`

The Manifest records every binary, its Kind and its settings. It's the source of truth: Ivaldi only appends to it, keeping your entries, comments and ordering, and you may edit it. CI reads it to build Deployables, and Brokkr will read it to deploy them. Edits take effect in CI with nothing to regenerate.

```yaml
version: 1
binaries:
  web:
    kind: http
    port: 8080
  nightly:
    kind: job
    schedule: daily
    base_image: registry.example.com/base:with-tools  # optional, Deployables only
  cli:
    kind: tool
```

Deployables are built with ko on `gcr.io/distroless/static-debian12:nonroot` unless `base_image` says otherwise. Set `build: dockerfile` on a Deployable to build it from your own `cmd/<name>/Dockerfile` instead. Ivaldi never generates one.

## Managed and Unmanaged files

**Managed files** are identical in every Project and you shouldn't edit them. Ivaldi writes them at `init`; keeping them current afterwards is a separate system's job.

- `ivaldi.mk`: the shared make targets `lint`, `test`, `build`, `run-<name>`, `install` and `setup-hooks`. Binaries are discovered from `cmd/` each time make runs.
- `.githooks/pre-commit`: runs `make lint test` before every commit. It needs golangci-lint v2 on `PATH`, and says how to install it if it's missing. `git commit --no-verify` skips it. `core.hooksPath` isn't cloned, so run `make setup-hooks` once in each fresh clone.

  To give an existing Go repo the same hook, including a Project created before Ivaldi generated it, use the `ivaldi-hooks` agent skill in `skills/ivaldi-hooks/`. Install it by symlinking that directory into your agent's skills directory, for example `ln -s "$PWD/skills/ivaldi-hooks" ~/.config/opencode/skills/ivaldi-hooks` from a clone of this repo.
- `.github/workflows/ci.yml`: calls the shared workflow in [worldtree-industries/workflows](https://github.com/worldtree-industries/workflows), pinned by commit SHA. It passes only the `HARBOR_USERNAME` and `HARBOR_PASSWORD` secrets and runs with read-only permissions.

**Unmanaged files** are written once, if missing, and then belong to you. Ivaldi never touches them again.

- `Makefile`, which includes `ivaldi.mk`. Add your own targets here.
- `.golangci.yml`: [maratori's golden config](https://github.com/maratori/golangci-lint-config) plus the Layer rules
- `README.md` skeleton, `.gitignore`, `go.mod` (Go 1.27)
- each Layer's `doc.go`
- each `cmd/<name>/main.go`
- `.forge.yaml` (Ivaldi only ever appends to it)

## CI and images

The shared workflow lints with golangci-lint, runs `go test -race ./...`, and builds the Deployables affected by a change:

- Pull requests build images without pushing them.
- Pushes to `main` push affected Deployables tagged `sha-<short>`.
- A `vX.Y.Z` tag pushes the Deployables affected since the previous tag, tagged `vX.Y.Z`.

A Deployable is affected when a Go package it depends on changes, its Manifest entry or base image changes, or its Dockerfile changes. Any change to `go.mod` or `go.sum` affects every Deployable. Images go to `registry.werewolves.fyi/library/<binary>`, using org-level Harbor secrets. Tools are never built as images.

## GitHub settings

As its last step, `init` applies three settings to the Project's GitHub repository:

- Only GitHub-owned actions, `worldtree-industries/*` and the third-party actions the shared workflow uses may run.
- The default `GITHUB_TOKEN` is read-only.
- GitHub Actions may not create or approve pull requests.

This needs `origin` to point at GitHub and `gh` to be authenticated with admin rights on the repository. If it can't apply them (offline, no repository yet, no rights), `init` still succeeds: it warns and prints the exact `gh` commands to run later. `add` never touches GitHub settings, and Ivaldi never creates repositories or secrets.

## Developing Ivaldi

Ivaldi is its own Project: its `.forge.yaml` declares `ivaldi` as a `tool`, and its `ci.yml`, `ivaldi.mk` and `.githooks/pre-commit` are the ones `init` generates. Its Unmanaged `Makefile` adds an `e2e` target. `.github/workflows/e2e.yml` (Unmanaged) also runs the end-to-end tests.

- `make test` runs the unit tests. They run offline.
- `make e2e` builds the real binary, generates a Project with one binary of each Kind, and requires it to build, vet and lint, and to commit through its pre-commit hook, which must block a lint failure. It needs the network, Go 1.27, and `golangci-lint` v2 and `actionlint` on `PATH`. Tests that need a missing linter are skipped, unless `IVALDI_REQUIRE_LINTERS=1` is set, which makes them fail instead (CI sets it).
- `make lint` runs golangci-lint. Also run `golangci-lint run --build-tags e2e` to lint the e2e tests.
- `make setup-hooks` turns on a pre-commit hook that runs `make lint test`.
- `docs/extending-ivaldi.md` covers adding a Kind and testing at the command seam.
