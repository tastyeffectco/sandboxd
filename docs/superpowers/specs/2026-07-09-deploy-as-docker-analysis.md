# Deploy-as-Docker — analysis (dev sandboxes + prod containers, same server)

**Date:** 2026-07-09
**Question:** can we run **dev in sandboxes** and **prod as deployed Docker
containers** (prod env vars) on the **same server**, ideally **without changing
core**, as a **console feature** — and how urgent / how far is it?

## Short answer

- **Yes — a "prod on the same server" loop is reachable with ~zero core changes**,
  by treating a deploy as *a forked app's always-on sandbox with prod env*. It
  makes the dev→prod story **complete** for one server.
- A **"real" docker-image deploy** (build an image, run a lean plain container,
  custom domain) is the cleaner long-term shape but needs a **small core
  Deployment primitive** — this is the roadmap's flagship *"one-click deploy."*
- **Urgency: medium, not now.** It completes the product, but 0.3 is the dev
  experience. Right home is **0.4** (MVP) → **0.5** (real deploy). Agrees with
  your "not very urgent."
- **The env feature we just designed is the enabler** — its env-file injection is
  exactly what feeds prod env into either path. Not wasted work; it's the
  foundation of deploy.

## Two meanings of "deploy as docker"

| | A — Prod-as-sandbox (reuse engine) | B — Prod-as-image (real deploy) |
|---|---|---|
| What runs | the app in a container that never sleeps (base image + runtimed, prod mode) | a **built image** run as a lean plain container (`docker run --env-file`) |
| Core change | **~none** (uses fork + always_on + env) | **small** — a Deployment primitive |
| Prod domain | preview URL now; custom domain needs a route | custom domain part of the primitive |
| Fidelity | "prod-ish" (carries dev tooling) | immutable, restart-safe, real prod |
| Effort | days (console) | weeks (core + console) |

## What already exists (verified) — the reuse surface

- **Docker + Traefik + networking** — the whole substrate a deploy needs.
- **`always_on` idle policy** (`handlers.go`) — a sandbox that never idle-stops
  and is exempt from moderate memory pressure → "prod = never sleep." ✅
- **Env injection at create** (`Env` → `docker --env`; the env feature extends it
  to an out-of-workspace env-file) → **prod env vars**. ✅
- **Fork → a new app** (`v1ForkApp`) snapshotting current code — the unit you
  deploy (you deploy a point-in-time, which is *correct*). ✅
- **Exec + files APIs** to run a production build/start.

## What does NOT exist (the gaps)

- ❌ **No image build / registry / deployment concept** (confirmed: zero code).
- ❌ **Custom/prod-domain routing** — the Traefik generator only emits
  `s-<id>-<port>.preview.<domain>` (`traefik/traefik.go`). There *is* a Traefik
  **file provider** (`traefik/dynamic`) that a prod route could use, but the
  console can't write that host dir — so custom domain needs a small seam.
- ⚠️ **One sandbox per app** (`v1_apps.go` → 409 "app already has a sandbox").
  So dev + prod can't be two sandboxes of the *same* app — prod is a **fork**.

## Path A — Prod-as-sandbox (ZERO core, ships as a console feature)

The console orchestrates a "Deploy to production" using only existing APIs (+ the
env feature):

1. **Fork the app** → `POST /v1/apps/{id}/fork` → a `<app>-prod` app snapshotting
   the current code.
2. **Spin its sandbox** with `{ env: <prod vars>, idle_policy: "always_on" }` →
   prod env injected, never sleeps, runs beside the dev app on the same server.
3. **Run the production build+start** (not the dev server) — via the manifest's
   `build`/`web` command or an exec call.
4. **URL:** it gets `s-<id>-3000.preview.<domain>` today. For a real prod host,
   either accept the preview URL (MVP), or add a Traefik dynamic-file route
   (small follow-on), or a future core custom-domain feature.

**This delivers dev + prod on one server, prod env, zero core.** Honest limits:
- Prod is still a **sandbox** (base image + runtimed) — heavier per deploy, carries
  dev tooling; not a hardened prod runtime.
- Not the literal "docker run/Dockerfile" image — it's the engine's container.
- **Custom domain** is the one thing this path can't fully self-serve.

Good enough for staging / prod-on-one-box / demos; a real MVP.

## Path B — Prod-as-image (small core: the "one-click deploy" primitive)

For a true build → lean container:

- **Build must be control-plane-side** — sandboxes have **no docker.sock** (by
  design), so they can't build an image. A core endpoint owns it:
  `POST /v1/apps/{id}/deploy { environment }` →
  1. build an image (the app's **Dockerfile** if present, else base image + baked
     workspace / a buildpack),
  2. `docker run` a plain container with **`--env-file <prod>`** (the env feature),
     `restart=unless-stopped`,
  3. attach a **Traefik route** for the custom domain,
  4. record it in a `deployments` table (redeploy / rollback / status).
- This is the immutable, restart-safe, real-prod story — the roadmap's headline.
  Effort: weeks (build pipeline + deployments lifecycle separate from sandboxes +
  routing).

## Urgency & distance

- **Urgency:** medium. It **completes** the product (build in dev → run in prod)
  and is already the roadmap's flagship next item. **Not urgent for 0.3** (dev
  experience). Target **0.4 = Path A MVP**, **0.5 = Path B**.
- **Distance:**
  - **Path A:** close — needs the **env feature** + a console Deploy flow (fork →
    always_on → prod env) + a domain decision. **No core change.** Days.
  - **Path B:** further — a core **Deployment** primitive (build + lean container +
    custom domain + table). Weeks.

## Recommendation

1. **Ship the env feature first** (already specced) — it's the prerequisite for
   both paths.
2. **0.4: Path A in the console** — "Deploy to production" = fork + always-on
   sandbox + prod env, preview URL (or a tiny dynamic-route helper). Fast,
   zero-core, validates demand, completes the story on one server.
3. **0.5: Path B** — the real Deployment primitive (image build + lean container +
   custom domain), reusing the env-file injection and Traefik. This is the true
   "deploy as docker."
4. **Custom domain** is the one shared gap — smallest useful core add is a
   route-registration seam over the Traefik file provider; can land with either
   path.

## Is it "complete"?

- Path A makes the **story** complete on one server (dev sandbox + always-on prod
  with prod env). It's "prod-ish."
- Path B makes it a **proper** deploy (immutable image, restart-safe, custom
  domain).
- **Multi-server / scaling / zero-downtime** is explicitly later (that's Coolify's
  territory) — out of scope for "for now, same server."
