# Releasing

The Git tag publishes the Go module version.

1. Check that the [API demo](https://factory.pdigger.com/developers/api/) and
   [recording downloads](https://factory.pdigger.com/developers/data/) are available. Run the Integration workflow
   manually. It downloads the recordings, verifies their SHA-256 checksums and compares the replay summaries.
   Continue when both checks pass.

2. Update `Version` in [version.go](version.go). In `CHANGELOG.md`, replace `Unreleased` in the version heading with
   the release date.

3. Merge to `main` and wait for CI to pass.
4. Tag the tested commit with `v` followed by the version, then push that tag. For version 0.1.0, run
   `git tag v0.1.0` followed by `git push origin v0.1.0`.

The [release workflow](.github/workflows/release.yml) checks the tag against `Version`, runs the Docker tests and
creates a GitHub release using the matching changelog section. It fetches the version through `proxy.golang.org` so
it is available to Go users and on pkg.go.dev. GitHub supplies the workflow token.
