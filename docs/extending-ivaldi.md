# Extending Ivaldi

How to add a Kind is in the package comment of `internal/kind/kind.go`, and `tool.go`, `job.go` and `http.go` are the worked examples. This file holds what the code doesn't tell you.

## Kinds

- `Questions.Binaries` and `Questions.Exists` include binaries **planned earlier in the same run**, not only what's on disk or in the Manifest. Use them for anything unique per Project: http's next free port scans `Binaries`, and the one-time `client` question checks `Exists("client/doc.go")`.
- Settings are written to the Manifest in the order `Ask` returns them, after `kind:`. Name each key with a `manifest.Setting*` constant, shared by the code that writes it and the code that reads it back. Templates still name the key as text: `{{ .Settings.port }}`.
- A Kind's `Modules` are fetched with `go get`, followed by `go mod tidy`. A failure there is a warning that lists the commands left to run, never an error, so a Project is never left half-written in a state `init` would refuse to touch.
- Templates target mjolnir as published at `@latest`. Read its API at the released tag (`gh api repos/worldtree-industries/mjolnir/contents/<path>?ref=<tag>`, or the module cache), because a local mjolnir clone may be well behind.

## Testing at the command seam

- Tests drive `cli.Run` with a temp Project dir, scripted stdin, and the `fakeRunner` in `internal/cli/cli_test.go`. `p.script(line, stdout)` sets a command's output, `p.inOwnRepo(origin)` makes the dir its own repo's top level, and `fakeRunner.fail` makes a command fail.
- Scripted stdin is one answer per line, in prompt order. For `init`: the module path (only without a `go.mod`), each discovered `cmd/<name>/` binary's Kind and follow-up answers (in name order), the new-binaries list and each new binary's Kind and follow-ups, one overwrite answer per existing file (in the order they're listed), then the go line answer (only when an existing `go.mod`'s go line is below 1.27). Send the `client` answer only when the prompt will be asked (no `client/` yet, and no earlier http binary in the run said yes). Otherwise the extra line answers the next prompt.
- Yes/no prompts take their default at end of input (overwrite: no, go line: yes), so a test can stop early. Prompts that need an answer (module path, Kind, port, schedule) fail at end of input, with nothing written.
- Anything that needs the network, such as building a binary against mjolnir, goes behind the `e2e` build tag so plain `go test ./...` stays offline. `make e2e` and what it needs are in the README's "Developing Ivaldi" section. Use it instead of running `ivaldi init` by hand.
