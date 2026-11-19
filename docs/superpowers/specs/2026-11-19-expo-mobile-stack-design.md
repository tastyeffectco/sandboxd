# Mobile apps (Android and iPhone) with Expo (design draft for approval)

Status: draft, needs the maintainer's yes (core: a new preset and template in
the base image). Date: 2026-11-19.

## Intent

People should be able to say "make me a mobile app" and get one the same way
as the other stacks: pick **Mobile app (Expo)** in Choose stack (or let
Automatic pick it from the idea), see it running at once, and try it on their
own phone. Today `stackFromIdea` answers "Native mobile isn't available" and
falls back to React / Vite.

## Goals, non-goals, anti-goals

- Goals: one more stack, same model as the others (template + `sandbox.yaml`
  + preset); preview in sandboxd as a phone-sized screen; open it on a real
  Android or iPhone by scanning a QR code with the free **Expo Go** app; the
  coding agents know how to work in it.
- Non-goals (first version): building store files (.apk/.aab/.ipa) and
  publishing to Google Play / App Store; native modules outside Expo Go.
- Anti-goals: an Expo account or any paid service to start; Android SDK, Java
  or Xcode in the base image (gigabytes for every sandbox); anything the user
  must configure by hand.

## Spike (2026-11-19, real base image `sandboxd-base:0.3.26`, uid 1000, no root)

- `create-expo-app` (Expo SDK 58, expo-router, React Native 0.88,
  react-native-web) + `pnpm install` with `node-linker=hoisted`: **14 s, 624 MB**
  `node_modules`.
- `expo start --web --port 3000`: **HTTP 200**, web bundle built (832 modules,
  5.5 s), 4 MB.
- The same port serves the **Expo Go manifest** (`expo-platform` header).
  With `EXPO_PACKAGER_PROXY_URL=https://<preview host>` the manifest's bundle
  URL, `hostUri` and `debuggerHost` all point at the HTTPS preview, so a phone
  loads the app through sandboxd's normal preview route. No second port.

## Design

### Core (the part to approve)

1. **Template** `image/templates/expo-standard`: the default Expo starter
   (expo-router), pinned SDK, `.npmrc` with `node-linker=hoisted`, a lockfile,
   and an `AGENTS.md` note: Expo Go-compatible packages only
   (`npx expo install <pkg>`), check the web preview, keep the app to port 3000.
2. **Preset** `expo` ("Mobile app (Expo)") in `internal/preset`:
   ```yaml
   version: 1
   web:
     command: "[ -x node_modules/.bin/expo ] || pnpm install; CI=1 EXPO_NO_TELEMETRY=1 pnpm exec expo start --web --port 3000"
     port: 3000
     health_path: "/"
   build:
     command: "pnpm exec expo export --platform web --output-dir /tmp/expo-check"
   ```
   `EXPO_PACKAGER_PROXY_URL` is set to the sandbox's own preview URL by
   runtimed when the preset is `expo` (it knows that URL), so the QR code works
   without user setup. The build check exports to `/tmp` so it never touches
   the running dev server.
3. **Base image**: nothing new to install (Node 22 + pnpm are already there).
   Only the template is added (a few KB; dependencies install on first start,
   like the other stacks).

### Console (client of `/v1`, no core)

- Choose stack lists **Mobile app (Expo)**; `stackFromIdea` maps "mobile app,
  android, iphone, ios app, react native, expo" to it instead of the
  "not available" warning (Flutter, Swift and Kotlin keep the warning and get
  Expo instead).
- The preview of an `expo` app shows in a phone frame, with **"Open on your
  phone"**: a QR code for `exps://<preview host>` and two lines: "Install Expo
  Go (free) from the App Store or Google Play, then scan this code."
- Where the preview isn't reachable from a phone (the desktop app on
  `localhost`), the button says so in plain words and offers the web preview
  only (see open question 2).

## Risks

- **Preview access**: Expo Go cannot sign in. It works when the app's preview
  is reachable without a login (sandboxd's public previews). Private previews
  need a tokenized URL (open question 3).
- **Expo SDK updates**: pinned in the template; bumping the SDK is a tested
  image change, like the other templates. Existing apps keep their own
  `package.json`.
- **Disk**: about 600 MB per mobile app's `node_modules` (similar to Next.js).

## Plan outline (after approval)

1. Template + preset + runtimed env, with tests (preset registry, manifest,
   env only for `expo`).
2. Console: stack entry, `stackFromIdea`, phone frame + QR, tests.
3. Verify end to end on a VPS with a real phone in Expo Go (Android and
   iPhone), then on the Windows desktop from a fresh install.
4. Docs: README stacks list, docs for mobile apps.
5. Later, behind its own approval: store builds with the person's own Expo
   account (EAS Build, free tier) as an optional connection.

## Open questions for the maintainer

1. OK to add the `expo` preset and template to core and the base image?
2. Desktop app: phones can't reach `localhost`. Ship web preview only on the
   desktop first, or route mobile previews through sandboxd Cloud's public
   URL?
3. Private previews: add a short-lived token in the QR URL so Expo Go can open
   it without a login?
4. Store builds (EAS) later as an optional connection: yes or no?
