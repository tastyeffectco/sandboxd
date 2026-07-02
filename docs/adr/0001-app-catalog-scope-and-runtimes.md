# ADR 0001 — App Catalog: scope, runtimes, and images

**Status:** Accepted (v1) · **Date:** 2026-07-02

## Context

We want the console to install curated open-source apps one-click, to showcase sandboxd's
"run anything in a hardened sandbox" core as a first use case — **without changing core.** A 94-app
QA sweep proved feasibility (92/94 live via the exact store flow, marker-verified, template-fakes
rejected). The open question: how do apps needing non-base runtimes (Java/PHP/.NET/Deno/Ruby) run,
and do we adapt base images?

## Decision

**1. The catalog is a console-side data layer over the public `/v1` API. Core is untouched.**
Each app = a `sandbox.yaml` (generic: `web.command → catalog-run.sh`, port 3000, health_path) + a
self-bootstrapping `catalog-run.sh`, written via `PUT /v1/sandboxes/{id}/files`, adopted via stop/start.
No preset, no per-app Go code, no new endpoints, no image awareness in core. Delete the catalog and core
doesn't notice.

**2. v1 launch scope = Node + Python + static-binary apps only (one base image).**
Ship the 77 apps that run on the stock `sandboxd-base:0.4.0` with **no foreign runtime dropped**:
- `binary` (41): download one pinned static Go/Rust binary, `chmod`, run. Simplest + most reliable.
- `node` (17): `npm/pnpm/bun install` on base Node 22.
- `python` (19): `uv venv` + `uv pip install` on base Python 3.13.

**3. Runtime-drop families are SHELVED, not deleted.** The 15 recipes that curl a foreign runtime into
the workspace — **Java** (metabase/keycloak/traccar via portable JRE), **.NET** (sonarr/prowlarr/duplicati),
**PHP** (dokuwiki/drupal/… via static-php), **Deno** (silverbullet) — live in `CATALOG_DEFERRED`, kept in
code, not surfaced. They move to **image profiles** later.

**4. Base-image strategy: one lean base + a few thin runtime layers — NOT one mega-image.**
The QA numbers drove this: baking every runtime into one image bloats the already-1.75 GB base to ~3 GB for
*every* sandbox (incl. the 41 binary apps and all non-catalog use cases) — worse cold-start, bigger attack
surface, rebuild-the-world coupling, and it doesn't even fix version diversity (per-app venvs still needed).
The correct model is per-family thin layers selected by the recipe:
- `sandboxd-base` (default) → the 77 v1 apps + all other use cases.
- `sandboxd-java` (+~180 MB JRE layer) → the 3 Java apps (a shared runtime re-downloaded per sandbox today —
  the one clear "bake it" case). **`sandboxd-ruby`** (already built) → Rails family, required (no portable Ruby).
- `.NET` stays self-contained (runtime is inside the app binary — an image buys ~nothing); PHP/Deno are
  small single binaries — pinned runtime-drop is fine, or fold into an image later.
Needs the core `image:` field + `SANDBOXD_IMAGES` allowlist; recipe declares `image?`, core validates.

## Why not the alternatives

- **One mega-image with all runtimes:** rejected — kitchen-sink anti-pattern; size/security/coupling cost
  borne by 100% of sandboxes to serve ~15%.
- **"Node/Python only" (38 apps):** rejected — cuts the 41 static binaries, which are *simpler* than
  Node/Python (no runtime, no build) and were the most reliable in QA. "Simplicity" points the other way.
- **Keep runtime-drop for the heavy families long-term:** rejected as permanent — it's non-deterministic
  (48/94 recipes resolved `latest`; upstream asset renames broke 5 apps mid-QA), a supply-chain liability
  (13 third-party hosts, checksums discarded), and re-downloads big runtimes per sandbox (JRE ×N, hit 92%
  disk). Fine as a v1 bridge; the production answer for the heavy families is pinned+verified shared
  runtimes via image profiles.

## Hardening (required before calling v1 production-ready)

1. **Pin versions — done.** `catalog-pins.ts` pins all 41 static binaries to exact versioned URLs + SHA256.
2. **SHA256 verify — mechanism done.** `verifiedDownload(id, out)` emits `curl <pinned>` + `sha256sum -c`
   that aborts on mismatch. **Remaining: wire each binary recipe to it + re-QA** (mechanical).
3. **Deterministic install guards — done.** Every recipe guards on `.catalog-installed` / `.venv` /
   `node_modules`; retry == restart.
4. **Mark heavy honestly — done.** `effort: instant|quick|build`; 2 flaky builds (searxng, web-check)
   excluded from v1.
5. **Browser-deep QA — partial.** Current QA is marker-based through the real preview (rejects template
   fakes); true headless-browser assertion is a follow-up.

## Consequences

- v1 ships **77 verified apps on one base image, zero core change** — the two projects can still split
  cleanly along the `/v1` seam.
- The `image:` field is the single core dependency that unlocks the shelved 15 (Java) + Ruby. It's the
  highest-value core ask; until then the shelf stays shelved.
- Agent-task journeys (install → develop-on-top / drive-the-app-API) work today via `AGENTS.md` + per-app
  `skills/` written at install (see APP-CATALOG-CONTRACT.md §9).
