# Getting help

- **How-to questions & ideas** → [GitHub Discussions](https://github.com/tastyeffectco/sandboxd/discussions).
- **Bugs** → open an issue with the [bug report template](.github/ISSUE_TEMPLATE/bug_report.yml).
- **Feature requests** → the [feature request template](.github/ISSUE_TEMPLATE/feature_request.yml).
- **Security vulnerabilities** → **do not** file a public issue; follow [SECURITY.md](SECURITY.md).

## Before you ask

- Skim the [README](README.md) and the docs in [`docs/`](docs/) (start with the
  [OpenAPI spec](docs/openapi.yaml) and [`docs/production-safety.md`](docs/production-safety.md)).
- Remember the split: **sandboxd core** is the control plane + `/v1` API; the
  **console** is one client on top of it. Say which one your question is about —
  it usually changes the answer.
- Include your version/commit, host + Docker version, and the relevant `.env`
  knobs (redact secrets).
