---
name: ivaldi-hooks
description: Install the worldtree pre-commit hook (`make lint test`, from Ivaldi) in an existing Go repo, including Ivaldi Projects created before Ivaldi generated it. Use when a Go repo needs a pre-commit hook, or its hook differs from Ivaldi's.
---

# Install the worldtree pre-commit hook

The hook is Ivaldi's **Managed** `.githooks/pre-commit`. Its single source of truth is `internal/templates/pre-commit` on `main` in worldtree-industries/ivaldi: fetch it every run, and write it byte-for-byte. The same goes for `internal/templates/ivaldi.mk`.

```bash
raw() { gh api -H 'Accept: application/vnd.github.raw' "repos/worldtree-industries/ivaldi/contents/internal/templates/$1"; }
```

Run from the repo's top level. Leave every change uncommitted for the user to review.

## Steps

1. **Classify the repo.** A `go.mod` is required; without one, stop and say so. It's an **Ivaldi Project** when both `.forge.yaml` and `ivaldi.mk` exist; otherwise it's a **plain Go repo**. Done when you can name which.

2. **Check for an existing hook.** Look at `.githooks/pre-commit`, `.git/hooks/pre-commit`, and `git config --get core.hooksPath` (a different path, such as `.husky`, is an existing hook too). Where one exists and differs from `raw pre-commit`, show the diff and replace it only on the user's say-so. Say what the old hook checked that the new one won't (for example `-race` or `go vet`), so the user can keep it in a Makefile target the hook calls. Done when there's no conflicting hook, or the user has decided.

3. **Write the files.**
   - **Ivaldi Project**: overwrite `ivaldi.mk` with `raw ivaldi.mk`. It's Managed, so replacing it is always safe. This brings `setup-hooks`.
   - **Plain Go repo**: the hook needs `make lint`, `make test` and `make setup-hooks`. Check each with `make -n <target>`. Add only the missing ones to the `Makefile`, creating it if there's none, and leave existing targets exactly as they are:
     ```make
     lint:
     	golangci-lint run ./...
     test:
     	go test ./...
     setup-hooks:
     	git config core.hooksPath .githooks
     ```
     Add the new targets to `.PHONY`.
   - **Both**: write `raw pre-commit` to `.githooks/pre-commit` and `chmod +x` it.

   Done when `make -n lint test setup-hooks` succeeds and `.githooks/pre-commit` matches `raw pre-commit` exactly (`diff <(raw pre-commit) .githooks/pre-commit`).

4. **Turn it on.** `make setup-hooks`. This sets `core.hooksPath` for every checkout of the repo, which is the point; mention it in the report.

5. **Prove it.** Run `.githooks/pre-commit` directly. Done when it exits 0, or you've reported what failed: lint findings or test failures in the repo itself, or a missing `golangci-lint` (the hook prints the install link). Report failures for the user to fix rather than fixing the repo's code.

## Report

Which kind of repo it was, the files changed (`git status --short`), any replaced hook and what it used to check, and the result of step 5.
