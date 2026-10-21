# Pure installer-policy tests. Network, signatures, WSL and execution are mocked.
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
$global:sandboxdInstallerTest = @{ Calls = @(); BadHash = $false; BadSignature = $false }
$global:sandboxdInstallerTest.BadHash = $false
$global:sandboxdInstallerTest.BadSignature = $false
function wsl.exe { $global:sandboxdInstallerTest.Calls += 'wsl'; $input | Out-Null; $global:LASTEXITCODE = 0 }
function Invoke-WebRequest {
    param($Uri, $OutFile, [switch]$UseBasicParsing)
    $global:sandboxdInstallerTest.Calls += 'download'
    if ($OutFile) { [IO.File]::WriteAllText($OutFile, 'fixture'); return }
    $arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64' -or $env:PROCESSOR_ARCHITEW6432 -eq 'ARM64') { 'arm64' } else { 'x64' }
    return @{ Content = ('a' * 64) + "  sandboxd-desktop-0.3.23-win-$arch.exe`n" }
}
function Get-FileHash { param($Algorithm, $Path) return @{ Hash = $(if ($global:sandboxdInstallerTest.BadHash) { 'b' * 64 } else { 'a' * 64 }) } }
function Get-AuthenticodeSignature { param($FilePath) return @{ Status = $(if ($global:sandboxdInstallerTest.BadSignature) { 'NotSigned' } else { 'Valid' }) } }
function Start-Process { param($FilePath, [switch]$Wait, [switch]$PassThru) $global:sandboxdInstallerTest.Calls += 'execute'; return @{ ExitCode = 0 } }
function Assert($condition, $message) { if (-not $condition) { throw $message } }
& "$root/install.ps1" -NoDesktop
Assert (($global:sandboxdInstallerTest.Calls -contains 'wsl') -and ($global:sandboxdInstallerTest.Calls -notcontains 'download')) 'NoDesktop must install only the engine.'
$global:sandboxdInstallerTest = @{ Calls = @(); BadHash = $false; BadSignature = $false }
& "$root/install.ps1" -DesktopOnly
Assert (($global:sandboxdInstallerTest.Calls -notcontains 'wsl') -and ($global:sandboxdInstallerTest.Calls -contains 'execute')) 'DesktopOnly must install the client without WSL.'
$global:sandboxdInstallerTest = @{ Calls = @(); BadHash = $false; BadSignature = $false }
& "$root/install.ps1"
Assert (($global:sandboxdInstallerTest.Calls -contains 'wsl') -and ($global:sandboxdInstallerTest.Calls -contains 'execute')) 'Default must install both engine and desktop.'
foreach ($failure in @('hash', 'signature')) {
    $global:sandboxdInstallerTest = @{ Calls = @(); BadHash = $false; BadSignature = $false }
    $global:sandboxdInstallerTest.BadHash = $failure -eq 'hash'; $global:sandboxdInstallerTest.BadSignature = $failure -eq 'signature'
    $rejected = $false
    try { & "$root/install.ps1" -DesktopOnly } catch { $rejected = $true }
    Assert ($rejected -and ($global:sandboxdInstallerTest.Calls -notcontains 'execute')) "Invalid $failure must block installer execution."
}
Write-Host 'PASS: Windows default installation, desktop opt-out, desktop-only, checksum and signature enforcement'

Remove-Variable -Name sandboxdInstallerTest -Scope Global
