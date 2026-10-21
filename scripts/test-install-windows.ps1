# Pure installer-policy tests. Network, signatures, WSL and execution are mocked.
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
$script:calls = @()
$script:badHash = $false
$script:badSignature = $false
function wsl.exe { $script:calls += 'wsl'; $input | Out-Null; $global:LASTEXITCODE = 0 }
function Invoke-WebRequest {
    param($Uri, $OutFile, [switch]$UseBasicParsing)
    $script:calls += 'download'
    if ($OutFile) { [IO.File]::WriteAllText($OutFile, 'fixture'); return }
    $arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64' -or $env:PROCESSOR_ARCHITEW6432 -eq 'ARM64') { 'arm64' } else { 'x64' }
    return @{ Content = ('a' * 64) + "  sandboxd-desktop-0.3.23-win-$arch.exe`n" }
}
function Get-FileHash { param($Algorithm, $Path) return @{ Hash = $(if ($script:badHash) { 'b' * 64 } else { 'a' * 64 }) } }
function Get-AuthenticodeSignature { param($FilePath) return @{ Status = $(if ($script:badSignature) { 'NotSigned' } else { 'Valid' }) } }
function Start-Process { param($FilePath, [switch]$Wait, [switch]$PassThru) $script:calls += 'execute'; return @{ ExitCode = 0 } }
function Assert($condition, $message) { if (-not $condition) { throw $message } }
& "$root/install.ps1" -NoDesktop
Assert (($script:calls -contains 'wsl') -and ($script:calls -notcontains 'download')) 'NoDesktop must install only the engine.'
$script:calls = @()
& "$root/install.ps1" -DesktopOnly
Assert (($script:calls -notcontains 'wsl') -and ($script:calls -contains 'execute')) 'DesktopOnly must install the client without WSL.'
$script:calls = @()
& "$root/install.ps1"
Assert (($script:calls -contains 'wsl') -and ($script:calls -contains 'execute')) 'Default must install both engine and desktop.'
foreach ($failure in @('hash', 'signature')) {
    $script:calls = @()
    $script:badHash = $failure -eq 'hash'; $script:badSignature = $failure -eq 'signature'
    $rejected = $false
    try { & "$root/install.ps1" -DesktopOnly } catch { $rejected = $true }
    Assert ($rejected -and ($script:calls -notcontains 'execute')) "Invalid $failure must block installer execution."
}
Write-Host 'PASS: Windows default installation, desktop opt-out, desktop-only, checksum and signature enforcement'
