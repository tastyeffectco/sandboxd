# sandboxd on Windows: Linux engine through WSL 2 + optional native Electron client.
# Requires Windows 11, WSL 2 (Ubuntu), Git/curl in WSL and Docker Desktop with
# Linux containers + WSL integration enabled for that distribution.
[CmdletBinding()]
param(
    [switch]$NoDesktop,
    [switch]$DesktopOnly,
    [switch]$NoConsole,
    [ValidateSet('auto', 'build', 'pull')][string]$Mode = 'auto',
    [ValidatePattern('^[A-Za-z0-9._-]+$')][string]$Distro = 'Ubuntu',
    [ValidatePattern('^[A-Za-z0-9._/-]+$')][string]$Ref = 'main',
    [ValidatePattern('^[0-9]+\.[0-9]+\.[0-9]+$')][string]$DesktopVersion = '0.3.23'
)
$ErrorActionPreference = 'Stop'
if ($DesktopOnly -and $NoDesktop) { throw 'DesktopOnly and NoDesktop cannot be combined.' }
if (-not $DesktopOnly) {
    if (-not (Get-Command wsl.exe -ErrorAction SilentlyContinue)) { throw 'Install WSL 2 with: wsl --install -d Ubuntu. Then enable Docker Desktop WSL integration and rerun.' }
    & wsl.exe --distribution $Distro --exec docker info *> $null
    if ($LASTEXITCODE -ne 0) { throw "Docker is not available in $Distro. Start Docker Desktop (Linux containers) and enable WSL integration for this distribution." }
    # Fetch and run inside the Linux home, preserving Linux bind-mount paths.
    # Pass a validated ref as an argument, never as executable PowerShell text.
    $bootstrap = @'
set -euo pipefail
cd ~
ref="$1"
mode="$2"
console="$3"
installer="$(mktemp)"
trap 'rm -f "$installer"' EXIT
curl --proto '=https' --tlsv1.2 -fsSL "https://raw.githubusercontent.com/tastyeffectco/sandboxd/$ref/install.sh" -o "$installer"
args=(--no-desktop)
[ "$mode" = auto ] || args+=("--$mode")
[ "$console" = yes ] || args+=(--no-console)
SANDBOXD_REF="$ref" bash "$installer" "${args[@]}"
'@
    $consoleMode = if ($NoConsole) { 'no' } else { 'yes' }
    # Avoid legacy PowerShell native-argument quoting: script travels via stdin.
    $bootstrap.Replace("`r", '') | & wsl.exe --distribution $Distro --exec bash -s -- $Ref $Mode $consoleMode
    if ($LASTEXITCODE -ne 0) { throw 'Engine installation failed. Fix the error above and rerun.' }
    Write-Host 'Engine ready. API: http://127.0.0.1:9090 (or the bind configured in WSL .env).'
}
if ($NoDesktop -or $env:SANDBOXD_DESKTOP -eq '0') { return }
$architecture = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64' -or $env:PROCESSOR_ARCHITEW6432 -eq 'ARM64') { 'arm64' } else { 'x64' }
$asset = "sandboxd-desktop-$DesktopVersion-win-$architecture.exe"
$base = "https://github.com/tastyeffectco/sandboxd/releases/download/desktop-v$DesktopVersion"
$temporary = Join-Path ([IO.Path]::GetTempPath()) ([Guid]::NewGuid().ToString())
New-Item -ItemType Directory -Path $temporary | Out-Null
try {
    $installer = Join-Path $temporary $asset
    Invoke-WebRequest -UseBasicParsing -Uri "$base/$asset" -OutFile $installer
    $checksums = (Invoke-WebRequest -UseBasicParsing -Uri "$base/SHA256SUMS").Content
    if ($checksums -is [byte[]]) { $checksums = [Text.Encoding]::UTF8.GetString($checksums) }
    $pattern = '(?m)^([a-fA-F0-9]{64})  ' + [regex]::Escape($asset) + '\r?$'
    $match = [regex]::Match($checksums, $pattern)
    if (-not $match.Success -or (Get-FileHash -Algorithm SHA256 $installer).Hash -ne $match.Groups[1].Value) { throw 'Desktop checksum mismatch or checksum is missing.' }
    if ((Get-AuthenticodeSignature -FilePath $installer).Status -ne 'Valid') { throw 'Desktop installer does not have a valid trusted signature.' }
    $process = Start-Process -FilePath $installer -Wait -PassThru
    if ($process.ExitCode -ne 0) { throw "Desktop installer exited with code $($process.ExitCode)." }
} catch {
    throw "Desktop installation did not finish. Any installed engine remains running. $($_.Exception.Message) See docs/desktop.md for release availability."
} finally {
    Remove-Item -LiteralPath $temporary -Recurse -Force
}
