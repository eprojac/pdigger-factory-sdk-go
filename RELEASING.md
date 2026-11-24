# Releasing

A Go module is released by its tag; there is nothing to upload and no secret to set.

1. Check that the site the docs, the examples and the integration workflow link to is live: the Swagger demo at
   https://factory.pdigger.com/developers/api/ and the recordings at https://factory.pdigger.com/developers/data/
   (`curl -fsSI https://factory.pdigger.com/developers/api/ https://factory.pdigger.com/developers/data/recordings.json`).
   Then run the Integration workflow by hand: its recordings job downloads every published recording, checks its
   sha256 and compares its replay with the published summary. Do not release while either fails.
2. Set `Version` in [version.go](version.go) (the user agent sends it) and give the version's section of
   `CHANGELOG.md` its date in place of `Unreleased`: `## <version> - <date>`.
3. Merge to `main` and wait for CI.
4. Tag and push: `git tag v<version> && git push origin v<version>`.

[.github/workflows/release.yml](.github/workflows/release.yml) then runs the unit tests in Docker, checks that the tag
is `v` + `Version`, creates the GitHub release with the changelog's section as its notes (with the workflow's own
`GITHUB_TOKEN`), and fetches the version through `proxy.golang.org`, which puts it on the proxy and on pkg.go.dev.
