# Contributing

Run `make test` for formatting, vet, race detection, coverage, examples and fixture comparisons. Run `make lint` for
staticcheck as well. [Test instructions](TESTING.md).

## The contract comes first

All four SDKs follow the [same behaviour contract](spec/SDK-DESIGN.md) and [HTTP API](spec/openapi.yaml). The
maintainers sync the specification, fixtures, camera measurements and architecture diagram from the shared project.
Open an issue or pull request here to propose a change to these shared files.

The tests compare event, activity and summary text with `fixtures/golden/` byte for byte. Use the API’s
`operationId` names in Go naming style. The contract also defines retries, stream recovery and interpreter counts.

## Code

- Go 1.23 or later, the standard library only: the SDK has no dependencies, and the tests none either.
- Every call that can block takes a `context.Context`; streams are `iter.Seq2` iterators that stop when the loop
  breaks or the context ends.
- One error type, `*Error`, with a stable `Code`; wrap causes so `errors.Is` and `errors.As` see them.
- Tests use the fake unit (`factorytest`), injected clocks and seeded randomness: no network, no real sleeps over
  50 ms, no timing-dependent assertions. New code comes with tests that keep coverage at 100 %.
- Doc comments on every exported name; they are the API reference.

## Docs and examples

Edit the program in `examples/` first, run its test, then copy the excerpt into the guide. `examples/docscheck`
checks sample text, relative links and unfinished placeholders. Regenerate output pictures with `make pictures`.

## Commits and pull requests

Keep each change focused. Explain what it changes and why. Add user-visible changes to the current unreleased
section of `CHANGELOG.md`, creating a section if needed.
