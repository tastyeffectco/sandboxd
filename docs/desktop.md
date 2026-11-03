# sandboxd desktop

The Electron desktop app is a client of the sandboxd engine, just like the web
console. It bundles the same project builder, agents, files, terminal, previews,
model connections and settings, with native window controls, menus, engine
connections and a compact desktop layout. Closing it leaves engine workloads
running. The engine stays outside Electron. On Windows, the desktop can provision and
start a dedicated WSL engine; remote connections still use an existing engine.

## Release availability

The desktop version is **0.3.24**. Native installer builds are introduced
with this feature; a signed `desktop-v0.3.24` release must be published before the
automatic download installers can succeed. Until then, use a source build below.
CI artifacts are review builds, not signed public releases. Do not disable
Gatekeeper or SmartScreen to install them.

Desktop releases use separate `desktop-v…` tags and are excluded from GitHub's
latest engine release. Existing `runtime-v…-preview…` bundles are experimental
engine releases, not desktop installers.

## macOS

With Git and Docker Desktop installed and running, use the normal installer:

```bash
curl -fsSL https://raw.githubusercontent.com/tastyeffectco/sandboxd/main/install.sh | bash
```

After starting the engine, it installs the desktop client by default under
`~/Applications/sandboxd.app` and opens it. The download is architecture-specific,
checksum-verified, code-signed and checked by Gatekeeper. Use `--no-desktop` or
`SANDBOXD_DESKTOP=0` to skip it. This is independent of `--no-console`.

```bash
./install.sh --no-desktop       # engine and web console
./install.sh --no-console      # engine and desktop on macOS
bash scripts/install-desktop.sh # client only, with an existing engine
```

If the optional desktop install fails, the engine remains running and the
installer prints a retry command. Existing desktop installations are preserved;
quit the app and use the signed release DMG to update. Apple Silicon and Intel
packages are built separately. Local Docker Desktop engine support retains its
existing best-effort status; the native client can also connect to a Linux server.

## Windows: install from the desktop

Open the Windows desktop app, choose **This computer**, and click **Install &
open workspace**. The app checks for a running local engine first. If none is
available, it enables WSL if needed, downloads a checksum-pinned Ubuntu Base
24.04.5 image, creates a dedicated WSL 2 distribution, installs Docker Engine and
Compose there, and builds the engine from a pinned public repository commit.
Docker Desktop and an existing Ubuntu installation are not required for this flow.

Windows 11 with hardware virtualization enabled, internet access and sufficient
free disk space is required. Setup downloads several GB; the first engine build
can take 15–30 minutes or longer on a slow connection. Windows may show a UAC
administrator prompt when enabling WSL. If Windows requires a reboot, restart
when convenient, reopen the app, and click **Continue setup**. The app never
reboots Windows automatically. Progress appears in the connection panel. Keep the
app open during setup; **Retry setup** resumes the same environment after a failure.

When ready, the app connects to `http://127.0.0.1:9090` automatically and asks you
to create the engine's first console password. Previews use port 8080. If ports
9090 or 8080 are occupied by another application, setup explains the conflict.
VPNs, managed Windows policies, disabled virtualization or custom WSL networking
can prevent setup or localhost forwarding; these failures remain visible and retryable.

The distribution is named `sandboxd-desktop-<random ID>` and stored under the app's
user-data directory in `local-engine/disk`. Its name and completed-install state
are saved in `local-engine/engine.json`. Workspaces and engine configuration remain
in that distribution. Other WSL distributions, Docker Desktop and global WSL
settings are not changed. Windows elevation runs only Microsoft's WSL installer;
engine provisioning runs as Linux root in the dedicated distribution.
The setup logs are `/var/log/sandboxd-setup.log` and `/var/log/sandboxd-docker.log`
inside that environment; they can contain private information and should not be
posted publicly without review. They are not streamed to the renderer.

Reopening the app with the managed local engine selected starts it again if
needed. Closing the app leaves the engine and workloads running. Setup never
unregisters a distribution or deletes workspaces. Removing the desktop app does
not intentionally remove engine data. Back up work before manually removing the
managed distribution or its user-data directory.

Each desktop release pins one engine release (tag and commit). When an updated
app opens and its managed engine runs another commit, it moves the engine to the
pinned release ("Updating your engine to the new version…"), only once that tag
is reachable and resolves to the pinned commit; offline, the current engine just
starts and the update waits for the next launch. Before updating, it stops the
stack and backs up `.env` and the engine state to
`/var/lib/sandboxd-desktop/backups/<timestamp>`, then installs the release's
signed images (up to three attempts). If the install fails or the updated engine
does not become ready, the previous version, settings and database are restored
and started again. An engine the user runs outside the managed distribution is
never updated by the app.

## Windows: command-line installation

Use Windows 11 with WSL 2 (Ubuntu), Git/curl inside Ubuntu, and Docker Desktop
configured for Linux containers with WSL integration enabled for Ubuntu. Open
PowerShell, download the installer, review it, then run it:

```powershell
Invoke-WebRequest https://raw.githubusercontent.com/tastyeffectco/sandboxd/main/install.ps1 -OutFile install.ps1
.\install.ps1
```

The script starts the Linux engine through the existing installer inside WSL,
then opens the native desktop setup wizard. The desktop option is on by default.
The wizard installs per user and offers desktop/Start menu shortcuts. It verifies
the release SHA-256 checksum and trusted Authenticode signature before execution.

```powershell
.\install.ps1 -NoDesktop               # engine and web console only
.\install.ps1 -DesktopOnly             # connect to an existing engine; no WSL needed
.\install.ps1 -Distro Ubuntu -Mode build
```

`SANDBOXD_DESKTOP=0` also skips the desktop. `-NoConsole` skips the web console.
The engine checkout, database and workspaces remain in the Linux filesystem;
the native app is a Windows client. The API normally forwards to
`http://127.0.0.1:9090` through Docker Desktop/WSL. Custom WSL networking or a custom
API bind may require a different address. If Docker is unavailable, the script
explains the prerequisite and stops; it never installs a hypervisor or reboots
Windows. WSL engine installation needs validation on your Docker Desktop setup;
CI tests native clients against a disposable API fixture, not a Windows hypervisor.

## Connect and build

1. On Windows, choose **This computer → Install & open workspace** for automatic
   local setup. Otherwise enter `http://127.0.0.1:9090` for a running local engine,
   or choose **Remote engine** for an existing HTTPS API origin. Use the engine API address, not an app preview.
2. Click **Open workspace**, then create your first console password or sign in
   with the existing one. The web console and desktop use the same engine account.
3. Connect your coding agent/model in Settings, describe a project and build.

Click the engine name in the title bar or use `Cmd/Ctrl+Shift+E` to switch engines.
`Cmd/Ctrl+K` opens search; `Cmd/Ctrl+N` returns to the project builder. Connections
are remembered. **Disconnect** leaves the profile and engine running; **Forget
this engine** removes the local profile and saved session. Signing out revokes
the session on the engine. Forgotten sessions are not remotely revoked.

Remote connections require HTTPS unless you explicitly enable unencrypted HTTP
in connection options. API requests use the bundled Node TLS trust store; private/self-signed
certificates may not work even when a browser trusts them. Certificate
verification is never disabled, and there is no certificate-exception switch. An offline
banner offers retry and connection settings. The app requires a reachable engine
for workspace actions; it is not an offline copy of your code.

## Your servers

Put your apps online on a Linux server you rent (Hetzner, DigitalOcean, OVH…).
Open **Settings → Your servers → Add a server**, paste the server address and
the password from your provider's email, and click **Add server**. Under
**More options** you can set a user name other than `root` (it must be able to
use `sudo`), another SSH port, and a name for the list. The app shows each step:
connecting, checking the server, installing sandboxd (a few minutes), securing
the connection. Keep the app open until it says the server is ready.

What the app checks first, with a plain message if one fails: the server runs
Linux on an x86-64 or ARM64 processor, has at least 5 GB of free disk space
and 1 GB of memory, and can download files (curl or wget). If Docker is
missing, the app says so and offers **Install Docker and continue**, which runs
Docker's official installer (`get.docker.com`) on the server; nothing is
installed before you click it.

How it works and what is kept:

- The install runs over SSH with the password you typed, which is held in
  memory for that one setup only. It is never written to disk, logs or progress
  messages. For a `sudo` user, it is sent to `sudo -S` on standard input, never
  on a command line. The server's own `install.sh` runs at the engine version this
  app pins (`SANDBOXD_SERVER_REF=<branch or tag>` overrides it for testing; a ref
  that is not a `v…` tag builds images instead of pulling signed ones).
- Each computer has its own SSH key (ed25519), created the first time you add a
  server. The private key is encrypted with the OS credential store (macOS
  Keychain, Windows DPAPI) in `device-key.json` in the app's data directory.
  After the install, its public key is added to the `~/.ssh/authorized_keys` of
  the login you typed, marked `sandboxd-desktop:<device id>`, so the app can
  sign in again without the password. Adding a server needs secure storage;
  Linux `basic_text` is refused.
- The server's SSH identity (host key, SHA256 fingerprint) is remembered the
  first time. If it changes later, the app stops talking to the server and
  says so. If you reinstalled the server yourself, click **I reinstalled it:
  trust the new identity** and confirm in the dialog that shows the old and new
  fingerprints. Otherwise, don't: someone may be impersonating your server.
- `servers.json` in the app's data directory lists your servers (address, port,
  pinned fingerprint, front door, the engine API token encrypted with the OS
  credential store). Both files are written atomically with mode 0600.
- The app keeps one SSH connection per server, with that login, and reconnects on
  its own after sleep, a network change or a server restart. The list shows
  **Connected**, **Connecting…** or **Offline** with the reason. The console
  reaches a server's engine through the private gateway at
  `/servers/<server id>/v1/…` (HTTP, SSE and terminals), which adds that server's
  API token. Unknown server ids are refused; no other host can be reached.

- Nothing else runs on the server unless you approve it. Changes beyond the
  install (for example connecting your nginx or Caddy) are proposed by the
  assistant in plain words, and run only after you click **Allow**; commands that
  need admin rights ask for the password again if the login needs one for sudo.

**Removing a server** forgets it on this computer, removes this computer's line
from the login's `authorized_keys` (when the server answers) and closes the
connection. sandboxd and your apps keep running on the server. If the server was
offline, the app says the line is still there; it is marked `sandboxd-desktop`.

## Build and test from source

Use Node.js 22 and pnpm 9.15.0. From a source checkout:

```bash
pnpm --dir console install --frozen-lockfile --ignore-scripts
npm --prefix desktop ci --ignore-scripts
node desktop/node_modules/electron/install.js
npm --prefix desktop test
npm --prefix desktop run build:ui
npm --prefix desktop start
```

On the target OS, `npm --prefix desktop run dist` builds the native installers in
`desktop/release`. `npm --prefix desktop run smoke` tests the real Electron window
using a disposable local engine fixture; `smoke:package` launches the fused
package. Both use temporary profiles. Linux is a development target, not a
published desktop installer target. GUI tests on Linux need Xvfb and working
Chromium sandbox support. A constrained test container can set
`SANDBOXD_TEST_NO_OS_SANDBOX=1` for the smoke harness only; this does not change
shipping app settings and does not validate the OS sandbox. macOS and Windows CI
run without that override.

## Security and local storage

- The renderer is sandboxed with Node disabled and context isolation enabled.
  Native IPC is limited to connection management, fixed Windows engine setup,
  your servers (add, remove, status, approved server changes), state
  and HTTP(S) links; only
  the packaged main frame may call it. Preview frames have no native bridge.
- Bundled assets come from the application archive. A private loopback gateway
  proxies only `/v1/` requests to the selected engine, including SSE and terminal
  WebSockets. A random capability header is injected by Electron for requests
  from the owned UI; no capability cookie is shared with localhost previews.
  Host, Origin and Fetch Metadata checks reject other clients and sites.
- Engine login cookies stay in the main process. Only the engine session cookie
  is forwarded; redirects, ambient browser cookies and proxy headers are blocked.
  Switching engines closes old streams and terminals.
- Profiles and encrypted sessions live in Electron's per-user application data
  directory (`sandboxd-desktop/connections.json`). Passwords are never stored.
  Session encryption uses Electron safeStorage (macOS Keychain / Windows DPAPI).
  If secure storage is unavailable, sessions remain in memory only. Linux's
  `basic_text` fallback is not used to persist credentials.
- Packaged fuses disable RunAsNode, Node options, the Node inspector and extra file
  protocol privileges; only the application ASAR is loaded, with archive integrity
  validation on supported platforms. External links open in your normal browser.
  Workspace exports use the authenticated gateway and a native save dialog.
- The renderer has no shell execution or generic filesystem bridge. Windows
  engine setup accepts no commands, paths, URLs or arguments from the renderer.
  Only a previously provisioned local engine can be automatically started. Ubuntu
  downloads are checked against bundled SHA-256 hashes before WSL import, and
  engine source is fetched at an immutable commit. No desktop auto-updater, custom
  URL protocol or desktop telemetry is added. Managed setup disables engine telemetry.

The Electron and electron-builder versions are pinned with an npm lockfile and
covered by Dependabot. The builder pin uses the maintained v26 line, including
[the NSIS archive decoder compatibility fix](https://github.com/electron-userland/electron-builder/pull/9989)
needed for executable files in Windows ARM64 installers (26.15.6 or newer). As of this implementation, `npm audit` reports the
unpatched moderate [sprintf-js precision DoS advisory](https://github.com/advisories/GHSA-hp3w-g68c-fv3c)
in the **build-tool dependency tree** (eight affected package paths). It is not a
runtime dependency and is not included in the packaged app. There are no high or
critical findings in that tree. Recheck this assessment when updating tooling;
do not apply npm's suggested tooling downgrade blindly.

## Maintainer release procedure

The `desktop` workflow builds and launches native clients on macOS ARM64/x64 and
Windows ARM64/x64, with API-boundary tests and installer policy checks. It also
installs the generated NSIS package / extracts the Mac ZIP into a disposable
runner directory and launches that installed app, verifying its fuses and archive.
The Windows setup state machine covers retries, restart prompts, download integrity
and isolation from other environments. A separate Windows WSL integration job
exercises actual provisioning and restart recovery on a disposable runner. Review its
screenshots and unsigned build artifacts before releasing.

Configure the `desktop-release` GitHub environment with:

- `MAC_CSC_LINK` (Developer ID Application certificate, base64 P12),
  `MAC_CSC_KEY_PASSWORD`, `APPLE_ID`, `APPLE_APP_SPECIFIC_PASSWORD`, `APPLE_TEAM_ID`.
- `WIN_CSC_LINK` (trusted Authenticode certificate, base64 PFX) and
  `WIN_CSC_KEY_PASSWORD`. Organizations using hardware-backed or cloud signing
  should adapt the signing step to their provider before publishing.

Update the desktop version in `desktop/package.json` and lockfile, both installer
version defaults, and installer tests together. After the reviewed commit reaches
main, push its matching `desktop-vX.Y.Z` tag. The release workflow requires a
mainline commit, runs native tests, signs/notarizes all packages, verifies their
signatures, tests the fused apps, and creates a **draft** with `SHA256SUMS`.
Missing signing credentials fail the release; unsigned artifacts are never
substituted. Review the draft, verify local engine connectivity on real Docker
Desktop/WSL installations, then publish it while keeping `latest=false` so engine
upgraders continue to see engine releases. No signing credentials are in source.
