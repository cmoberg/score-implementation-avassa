# score-implementation-avassa

Score → Avassa converter CLI. It reads one or more Score workload files and emits Avassa Application manifests, with placeholder expansion, simple resource priming, and useful conversion defaults.

## Features

1. `init` and `generate` subcommands.
    - `generate --overrides-file` and `generate --override-property` to apply Score overrides before conversion.
    - `generate --image` to supply an image when a container declares `image: "."` in Score.
    - Placeholder support for `${metadata...}` and `${resource...}` in variables, files, and resource params.
2. Local state stored in `.score-implementation-avassa/`.
3. Emits Avassa Application specs (services/containers) from Score workloads.

## Install and Build

- Build the CLI: `make build` (binary at `./score-implementation-avassa`).
- Print version: `go run ./cmd/score-implementation-avassa --version`.
- Test locally: `make test`.
- Optional container image: `make build-container` then `make test-container`.

## Usage

Common flows:

1) Initialize a project (creates `.score-implementation-avassa/` and a starter `score.yaml` if missing):

```sh
./score-implementation-avassa init
# or: go run ./cmd/score-implementation-avassa init
```

2) Generate Avassa manifests from a Score file:

```sh
./score-implementation-avassa generate -o manifests.yaml -- score.yaml
# Use '-' to write to stdout instead of a file:
./score-implementation-avassa generate -o - -- score.yaml
```

3) Multiple workloads (combined into a single output with `---` separators):

```sh
./score-implementation-avassa generate -o manifests.yaml -- app1.yaml app2.yaml app3.yaml
```

4) Apply overrides to a single Score file:

```sh
# Merge an overrides file
./score-implementation-avassa generate -o manifests.yaml --overrides-file overrides.yaml -- score.yaml

# Set or remove specific properties (repeatable)
./score-implementation-avassa generate -o manifests.yaml \
  --override-property metadata.labels.tier=prod \
  --override-property containers.main.image="stefanprodan/podinfo:latest" \
  --override-property service.ports.web.port=8080 \
  -- score.yaml

# Remove a field (empty value clears it)
./score-implementation-avassa generate -o manifests.yaml \
  --override-property metadata.annotations.avassa.approle= \
  -- score.yaml
```

5) Supply an image for containers that use `image: "."` in Score:

```sh
./score-implementation-avassa generate -o manifests.yaml --image my-registry/my-image:tag -- score.yaml
```

Notes:
- Run `init` once per workspace to create the state directory.
- When passing more than one Score file, override flags (`--overrides-file`, `--override-property`, `--image`) are not allowed.
- Use `--` before file paths to avoid ambiguity with flags.

## Avassa Mapping

- Containers: Score container variables become Avassa `env`; content/files are resolved and inlined with expansion by default.
- Application defaults (can be overridden via `metadata.annotations` on the Score workload):
  - `avassa.on-mutable-variable-change` (default: `restart-service-instance`).
  - `avassa.network` (sets `shared-application-network`).
  - `avassa.replicas` (default: `1`).
  - `avassa.share-pid-namespace` (default: `false`).
  - `avassa.log-size` (default: `100 MB`).
  - `avassa.log-archive` (default: `false`).
  - `avassa.shutdown-timeout` (default: `10s`).
  - `avassa.approle` (optional).
  - `avassa.on-mounted-file-change-restart` (if `true`, sets `on-mounted-file-change: { restart: true }`).

## Quick Start Example

Create `score.yaml`:

```yaml
apiVersion: score.dev/v1b1
metadata:
  name: example
  annotations:
    avassa.log-size: 100 MB
containers:
  main:
    image: stefanprodan/podinfo
    variables:
      DYNAMIC: ${metadata.name}
```

Run conversion:

```sh
./score-implementation-avassa init
./score-implementation-avassa generate -o manifests.yaml -- score.yaml
```

Example output (excerpt):

```yaml
---
name: example
services:
  - name: example-service
    mode: replicated
    replicas: 1
    share-pid-namespace: false
    containers:
      - name: main
        image: stefanprodan/podinfo
        container-log-size: 100 MB
        shutdown-timeout: 10s
        mounts: []
        env:
          DYNAMIC: example
on-mutable-variable-change: restart-service-instance
```

## Development

- Build: `make build`
- Test: `make test`
- Container: `make build-container` and `make test-container`

## Licensing

Code in this repository includes Apache 2.0 license headers and portions adapted from `score-compose` (Apache 2.0). Retain the Apache license and attribution in modified files.
