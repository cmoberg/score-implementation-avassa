# Repository Guidelines

## Project Structure & Module Organization
- `cmd/score-implementation-avassa/`: CLI entrypoint (`main.go`).
- `internal/command/`: Cobra commands (`init`, `generate`) and tests (`*_test.go`).
- `internal/convert/`: Workload → manifest conversion and placeholder expansion.
- `internal/provisioners/`: Resource priming/provisioning stubs.
- `internal/state/`: Local project state in `.score-implementation-avassa/state.yaml`.
- `internal/version/`: Build metadata and version string handling.
- Root: `Makefile`, `Dockerfile`, `go.mod`, `README.md`.

## Build, Test, and Development Commands
- `make build`: Build the CLI from `./cmd/score-implementation-avassa/` (binary: `./score-implementation-avassa`).
- `make test`: Run `go vet` and `go test ./...` with race detector and coverage.
- `go run ./cmd/score-implementation-avassa --version`: Print version for quick checks.
- `make test-app`: Build, then demo `init` and `generate` locally.
- `make build-container`: Build Docker image `score-implementation-avassa:local`.
- `make test-container`: Smoke test the containerized CLI.
Example local flow:
```
make build
./score-implementation-avassa init
./score-implementation-avassa generate -o manifests.yaml -- score.yaml
```

## Coding Style & Naming Conventions
- Go 1.21+ idioms; format with `gofmt`/`go fmt`; lint with `go vet`.
- Packages: short, lowercase; exported identifiers use PascalCase.
- Tests: `*_test.go`, descriptive `TestXxx` names; table-driven where sensible.
- CLI flags are kebab-case; files use snake/kebab (`score.yaml`, `manifests.yaml`).

## Testing Guidelines
- Frameworks: standard `testing` with `testify` (`assert`, `require`).
- All tests: `make test` or `go test ./...`.
- Focused run: `go test ./internal/command -run TestInitAndGenerate_with_sample`.
- Keep coverage for new code; race detector runs via `make test`.

## Commit & Pull Request Guidelines
- Use Conventional Commits: `feat:`, `fix:`, `docs:`, `test:`, `chore:`.
- PRs include: clear description, linked issues, reproduction/verification steps, and sample output/screenshot where relevant.
- Do not commit local outputs: `.score-implementation-avassa/`, `manifests.yaml`, `score.yaml`, or built binaries (see `.gitignore`).

## Security & Configuration Tips
- State is written to `.score-implementation-avassa/`; treat as ephemeral.
- Avoid secrets in `score.yaml` or overrides; prefer env vars and CI secrets.
