# Contributing

Thank you for improving the SDK. A change is ready when `make test` passes: it runs gofmt, go vet, every test with
the race detector and 100 % statement coverage, the golden tests, the examples and the picture check, in Docker,
with no network. `make lint` adds staticcheck.

## The contract comes first

This SDK is one of four (Go, Python, Rust, TypeScript) that follow one contract,
[spec/SDK-DESIGN.md](spec/SDK-DESIGN.md), and the API document, [spec/openapi.yaml](spec/openapi.yaml). Both, and the
fixtures, are shared by the four languages and change in all four at once: do not edit `spec/`, `fixtures/`,
`docs/BEHAVIOUR-20.20.md` or `docs/img/architecture.*` here; open an issue or a pull request here and the maintainers
make the change in all four. In particular:

- the sentences of events, activities and the summary are golden-tested byte for byte against `fixtures/golden/`;
  a change to the text is a change to the contract;
- names follow the API's `operationId`s and the contract's words (source, session, frame, layer, world, event,
  activity), in Go's case;
- the behaviour (what is retried, how streams resume, what the interpreter counts) is the contract's; the shape
  (options, iterators, errors) is Go's.

## Code

- Go 1.23 or later, the standard library only: the SDK has no dependencies, and the tests none either.
- Every call that can block takes a `context.Context`; streams are `iter.Seq2` iterators that stop when the loop
  breaks or the context ends.
- One error type, `*Error`, with a stable `Code`; wrap causes so `errors.Is` and `errors.As` see them.
- Tests use the fake unit (`factorytest`), injected clocks and seeded randomness: no network, no real sleeps over
  50 ms, no timing-dependent assertions. New code comes with tests that keep coverage at 100 %.
- Doc comments on every exported name; they are the API reference.

## Docs and examples

Every Go block of `README.md` and `docs/` is copied from a program in `examples/`, which the tests run against the fake
unit; `examples/docscheck` fails when a block is not found word for word, when a relative link is broken, and when a doc
has a placeholder left (an upper-case token such as `JUL30_END`, or `TODO`, outside code). To change a sample, change
the example (and its test) first, then copy it. The pictures in `docs/img/` are drawn by `make pictures` from what the
SDK prints; never edit them by hand.

## Commits and pull requests

Keep a change to one subject, say in the message what it changes and why, and add a line to `CHANGELOG.md` for
anything a user would notice, in the section that says `Unreleased` (start a `## Unreleased` section if there is none).
