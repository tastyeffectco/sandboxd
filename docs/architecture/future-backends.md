# Future backend readiness

**TL;DR for OSS users:** you need nothing in this document. sandboxd runs on
**Docker + SQLite only**. Every "enterprise" backend below is optional, absent
by default, and not something you install or configure to run the product.

**TL;DR for serious operators:** the three subsystems most likely to need a
heavier backend at scale — the container runtime, secret storage, and the event
timeline — are already isolated behind a single, swappable boundary each. When
you need Firecracker, Vault, or ClickHouse, you replace one component; you do not
rewrite sandboxd.

## Policy: concrete until the second backend exists

We deliberately do **not** ship speculative provider interfaces. A subsystem with
one implementation is a concrete type, not an `interface`. An abstraction is
introduced only when a second backend is actually being implemented — that's when
you know the right seam; designing it earlier tends to produce the wrong one and
taxes every OSS user with indirection they don't need. This keeps the default
small and honest while leaving a clear, cheap path to swap.

(The one place a small interface already exists — `events.Store` — is there
because the recorder needed *a* persistence method and an interface avoided an
import cycle, not as a speculative provider seam.)

## The three seams (as they exist today)

### Runtime — `internal/docker`
- **Today:** `docker.Client` is a thin, typed wrapper around the `docker` CLI
  (`os/exec`). It encodes only the CLI invocation; all policy (the hardened
  `RunSpec` flag set) lives in the caller. The control plane talks to the runtime
  *only* through this one type.
- **Future:** Firecracker / Kata / gVisor / remote workers / a k8s Job/Pod
  backend. Each is "swap the wrapper," not a rewrite — extract a small runtime
  interface (`Run/Stop/Exec/Inspect/Remove`) from `docker.Client` **when the
  second backend lands**, and inject it into the API server. The README already
  frames a k8s backend as "an interface swap, not a rewrite."
- **OSS default:** local Docker daemon over the mounted socket. No remote workers.

### Secrets — `internal/secrets`
- **Today:** `secrets.Cipher` encrypts app config/secrets at rest with
  standard-library AES-256-GCM. The master key comes from `SANDBOXD_SECRETS_KEY`
  or an auto-generated `0600` keyfile under the data dir — **no external service**.
  The API holds one `*secrets.Cipher`.
- **Future:** HashiCorp Vault / AWS Secrets Manager / GCP Secret Manager / KMS.
  Swap `Cipher` (or introduce a `SecretsBackend` with `Seal/Open`, or a broker
  that fetches by reference) **when implementing the managed backend**. The
  app-config `access_policy` field (`control_plane_only` default; `agent_access`
  / `runtime_access` / `both` reserved) already names the future delivery-broker
  direction without building it.
- **OSS default:** local key, encrypted-at-rest in the same SQLite DB.

### Events — `internal/events` + `app_events`
- **Today:** `events.Recorder` is the single choke point for the durable activity
  timeline; it writes one append-only `app_events` row in the existing SQLite DB.
  The table is **export-ready on purpose**: stable machine-readable `type` names,
  a ULID `id` that is also the page cursor, small valid-JSON payloads, and enough
  ids (app/sandbox/task/snapshot) to join — and it never stores secrets or raw
  command output.
- **Future:** ClickHouse / OTEL / Loki / a separate event DB. The cheapest first
  step is a JSONL export/dump from `app_events`; a second sink (tee the recorder)
  or an exporter process comes **when volume demands it**. Because the recorder is
  the only writer, adding a sink is local.
- **OSS default:** local SQLite table, no pruning yet (documented future knob
  `SANDBOXD_EVENT_RETENTION_DAYS`).

## Guarantees that keep this honest
- **No enterprise backend is required for a default install** — Docker + the
  Compose plugin, nothing else. Secrets auto-generate a key; events use the local
  DB; the runtime is the local Docker daemon.
- **The e2e CI job proves it**: it builds the base image and drives the real
  create → seed → serve → wake lifecycle on a plain Docker daemon with SQLite —
  no Vault, no ClickHouse, no remote workers.
- **Adding a backend is additive**: a new runtime/secrets/events backend is opt-in
  via config and does not change the default code path.

## Explicitly NOT in scope now
- No runtime provider interface, no Firecracker/Kata/remote-worker code.
- No Vault / cloud-secret-manager integration, and no secrets delivery broker.
- No ClickHouse / OTEL / Loki, no `EventSink` abstraction, no second event DB.

These land — with their interface, if one is even needed — the day a second
backend is actually built. Until then, sandbox­d stays Docker + SQLite simple.
