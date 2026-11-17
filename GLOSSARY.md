# Ivaldi

Ivaldi sets up and grows opinionated Go repositories: it creates the project skeleton and adds new binaries to it over time.

## Language

**Project**:
A single repository managed by Ivaldi, holding one or more related binaries that belong to the same domain.
_Avoid_: Repo (when meaning the managed whole), workspace, app

**Adoption**:
Running `ivaldi init` in a repository that already has code, which makes it a Project: existing `cmd/<name>/` binaries are discovered and recorded in the Manifest, and existing files are only overwritten with the user's yes.
_Avoid_: migration, import, conversion

**Binary**:
A build artifact of the project: the output of `go build` for one entry under `cmd/`. Every binary is either a Deployable or a Tool.
_Avoid_: Executable, app, command

**Deployable**:
A binary packaged into a container and released into an environment by a GitOps pipeline.
_Avoid_: Service, image

**Tool**:
A binary packaged for local use on a developer's machine.
_Avoid_: Binary (for this narrower meaning), CLI

**Kind**:
What a binary is: `http` (a long-running HTTP server), `job` (runs once to completion) or `tool`. A binary's Kind decides whether it is a Deployable or a Tool. The set of Kinds is built into Ivaldi.
_Avoid_: Type, binary type, template

**Layer**:
One of the project's top-level packages, each with a fixed role and a fixed set of layers it may depend on: `api`, `service`, `data`, `models` and `client`.
_Avoid_: Module (a Go module is something else), tier

**api**:
The layer that handles inbound requests: transport, routing, request and response shapes. It calls `service`.
_Avoid_: Handlers, controllers, contract

**service**:
The layer that holds the project's business logic.
_Avoid_: Domain, logic, core

**data**:
The layer that reads and writes persistent state and external stores.
_Avoid_: Repository, storage, dao

**models**:
The layer that holds the project's domain types. It depends on no other layer, and other projects may import it.
_Avoid_: Types, entities, dto

**client**:
The layer other projects use to call this project. It may depend only on `models`, and other projects may import it. Only projects with an `http` binary have one, and only when the user asks for it.
_Avoid_: SDK, api (that's the inbound side)

**Manifest**:
The project's record of its binaries, each one's Kind, and its per-binary settings (such as an `http` port or a `job` schedule). Ivaldi adds to it, the user may edit it, and Brokkr reads it to deploy the project's Deployables.
_Avoid_: Config, spec

**Brokkr**:
The GitOps pipeline (not yet built) that reads a project's Manifest and deploys its Deployables into an environment.
_Avoid_: Forge (only the Manifest's file name), the pipeline

**Managed file**:
A file that's identical in every project and that users don't edit. Ivaldi writes it at init; keeping it current afterwards is a separate system's job.
_Avoid_: Generated file (every file Ivaldi writes is generated)

**Unmanaged file**:
A file Ivaldi writes once, as a starting point, and never touches again. The user owns it from then on.
_Avoid_: Seeded file, template (that's the source, not the output)
