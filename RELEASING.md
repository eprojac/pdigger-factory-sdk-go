# Releasing

The Git tag publishes the Go module version.

1. Check that the [API demo](https://factory.pdigger.com/developers/api/) and
   [recording downloads](https://factory.pdigger.com/developers/data/) are available. Run the Integration workflow
   manually. It downloads the recordings, verifies their SHA-256 checksums and compares the replay summaries.
   Continue when both checks pass.

2. Update `Version` in [version.go](version.go). In `CHANGELOG.md`, replace `Unreleased` in the version heading with
   the release date.

3. Merge to `main` and wait for CI to pass.
4. Tag the tested commit with `v` followed by the version, then push that tag. For version 1.0.0, run
   `git tag v1.0.0` followed by `git push origin v1.0.0`.

The [release workflow](.github/workflows/release.yml) checks the tag against `Version`, runs the Docker tests and
creates a GitHub release using the matching changelog section. It fetches the version through `proxy.golang.org` so
it is available to Go users and on pkg.go.dev. GitHub supplies the workflow token.

## Publish on GitHub only

Before pushing the tag, set the repository Actions variable `SDK_GITHUB_ONLY_TAG` to the full tag name, including
the leading `v`. The workflow tests the tag and checks that Go can fetch the module. Publish the GitHub release
under the same tag with your own notes and images.
