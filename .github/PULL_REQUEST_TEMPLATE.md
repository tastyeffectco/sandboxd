<!-- Thanks for contributing to sandboxd! -->

## What & why

<!-- What does this change, and why? Link any issue: Closes #123 -->

## Component

<!-- Tick all that apply -->

- [ ] **sandboxd core** — control plane / API / `runtimed` / engine
- [ ] **console** — the web client (a client of `/v1`, not the core)
- [ ] App Store recipe / runtime detection
- [ ] docs
- [ ] CI / tooling

> Reminder: the console is a client on top of the `/v1` API. A console change
> should not require a core change unless the core genuinely lacks the
> capability — call it out if it does.

## Checklist

- [ ] Tests added/updated (`make test`)
- [ ] `make lint` / `gofmt` clean; console typechecks
- [ ] Docs updated if behavior or the API changed (incl. `docs/openapi.yaml`)
- [ ] CHANGELOG entry under **Unreleased** if user-facing

## How I verified

<!-- The commands or steps you ran to confirm this works end-to-end. -->
