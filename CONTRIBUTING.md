# Contributing

## Prerequisites

- Go 1.23 or later
- git

## Test and lint

Run the tests with the race detector:

```sh
make test
```

Run `gofmt`, `go vet`, and `staticcheck`:

```sh
make lint
```

Prompt templates have golden files in `internal/prompt/testdata`. After you change a template, regenerate the golden files and review the diff:

```sh
go test ./internal/prompt -update
```

## Commit messages

Commit messages must follow Conventional Commits. The release changelog groups commits by type.

## Releases

A maintainer cuts a release by pushing a `vX.Y.Z` tag. The release workflow runs the tests, builds the binaries with GoReleaser, and publishes them to the [Releases page](https://github.com/TannerWhite/aicommit/releases).

```sh
git tag v1.2.3
git push origin v1.2.3
```
