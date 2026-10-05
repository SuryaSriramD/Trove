# Requires only Windows PowerShell supplied with Windows; no package downloads.
. (Join-Path $PSScriptRoot 'package.ps1')
Assert-DesktopUser
$paths = Get-TrovePaths
$manifest = Read-TrovePackage $PSScriptRoot
$reviewedHash = (Get-FileHash -LiteralPath (Join-Path $PSScriptRoot 'desktop-manifest.json') -Algorithm SHA256).Hash
if ((Test-Path -LiteralPath $paths.App) -or (Test-Path -LiteralPath $paths.Link) -or (Test-Path -LiteralPath $paths.Registry)) { throw 'Existing application or URL registration preserved. Use its owning uninstaller first.' }
# Wails repeats the actual runtime check at launch, using its error-only strategy.
Assert-WebView2 $manifest.buildHost.pins.minimumWebView2
$parent = Split-Path -Parent $paths.App
New-Item -ItemType Directory -Path $parent -Force | Out-Null
$stage = Join-Path $parent ('.trove-install-' + [Guid]::NewGuid().ToString())
New-Item -ItemType Directory -Path $stage | Out-Null
try {
    Get-ChildItem -LiteralPath $PSScriptRoot -Force | Copy-Item -Destination $stage -Recurse
    $null = Read-TrovePackage $stage
    if ((Get-FileHash -LiteralPath (Join-Path $stage 'desktop-manifest.json') -Algorithm SHA256).Hash -ne $reviewedHash) { throw 'Package changed during installation.' }
    $receipt = @{ schemaVersion=1; appId='local.trove.desktop'; destination=$paths.App; installId=[Guid]::NewGuid().ToString(); manifestSHA256=(Get-FileHash -LiteralPath (Join-Path $stage 'desktop-manifest.json') -Algorithm SHA256).Hash }
    $receipt | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $stage '.trove-install.json') -Encoding UTF8
    if ((Test-Path -LiteralPath $paths.App) -or (Test-Path -LiteralPath $paths.Link) -or (Test-Path -LiteralPath $paths.Registry)) { throw 'An application registration appeared during installation.' }
    Move-Item -LiteralPath $stage -Destination $paths.App
    $stage = $null
    New-Item -Path $paths.Registry | Out-Null
    # Write ownership first; uninstall tolerates later missing registration nodes.
    New-ItemProperty -LiteralPath $paths.Registry -Name 'TroveInstallId' -Value $receipt.installId -PropertyType String | Out-Null
    Set-Item -LiteralPath $paths.Registry -Value 'URL:Trove workspace link'
    New-ItemProperty -LiteralPath $paths.Registry -Name 'URL Protocol' -Value '' -PropertyType String | Out-Null
    New-Item -Path ($paths.Registry+'\shell\open\command') -Force | Out-Null
    $exe = Join-Path $paths.App 'bin\Trove.exe'
    Set-Item -LiteralPath ($paths.Registry+'\shell\open\command') -Value ('"'+$exe+'" "%1"')
    $shell = New-Object -ComObject WScript.Shell
    $shortcut = $shell.CreateShortcut($paths.Link)
    $shortcut.TargetPath = $exe
    $shortcut.WorkingDirectory = Join-Path $paths.App 'bin'
    $shortcut.Save()
    Write-Host ('Installed '+$paths.App+'. Workspaces and model caches remain separate.')
} finally {
    if ($stage -and (Test-Path -LiteralPath $stage)) { Remove-Item -LiteralPath $stage -Recurse -Force }
}
