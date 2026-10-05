. (Join-Path $PSScriptRoot 'package.ps1')
Assert-DesktopUser
$paths = Get-TrovePaths
if (-not (Test-Path -LiteralPath (Join-Path $paths.App '.trove-install.json'))) { throw 'No owned Trove installation found.' }
$receipt = Get-Content -LiteralPath (Join-Path $paths.App '.trove-install.json') -Raw | ConvertFrom-Json
if ($receipt.schemaVersion -ne 1 -or $receipt.appId -ne 'local.trove.desktop' -or $receipt.destination -ne $paths.App -or $receipt.installId -notmatch '^[a-f0-9-]{36}$' -or $receipt.manifestSHA256 -ne (Get-FileHash -LiteralPath (Join-Path $paths.App 'desktop-manifest.json') -Algorithm SHA256).Hash) { throw 'Installation ownership differs; nothing removed.' }
$null = Read-TrovePackage $paths.App $true
$exe = Join-Path $paths.App 'bin\Trove.exe'
if (Test-Path -LiteralPath $paths.Registry) {
    $key = Get-Item -LiteralPath $paths.Registry
    if ($key.GetValue('TroveInstallId') -ne $receipt.installId) { throw 'URL registration belongs to another installation; preserved.' }
    # An interrupted owned install may not have created the launch command yet.
    $command = Get-Item -LiteralPath ($paths.Registry+'\shell\open\command') -ErrorAction SilentlyContinue
    if ($command -and $command.GetValue('') -ne ('"'+$exe+'" "%1"')) { throw 'URL launch command belongs to another installation; preserved.' }
    if ($null -ne $key.GetValue('') -and $key.GetValue('') -ne 'URL:Trove workspace link') { throw 'Changed URL registration preserved.' }
    if ($null -ne $key.GetValue('URL Protocol') -and $key.GetValue('URL Protocol') -ne '') { throw 'Changed URL protocol value preserved.' }
    foreach ($name in $key.GetValueNames()) { if (@('','URL Protocol','TroveInstallId') -notcontains $name) { throw 'Additional URL registration values preserved.' } }
    $allowed = @('shell','shell\open','shell\open\command')
    foreach ($item in Get-ChildItem -LiteralPath $paths.Registry -Recurse) {
        $relative = $item.Name.Substring($key.Name.Length+1)
        if ($allowed -notcontains $relative) { throw 'Additional URL registration data preserved.' }
        foreach ($name in $item.GetValueNames()) { if ($relative -ne 'shell\open\command' -or $name -ne '') { throw 'Additional URL registration values preserved.' } }
    }
}
if (Test-Path -LiteralPath $paths.Link) {
    $shell = New-Object -ComObject WScript.Shell
    $shortcut = $shell.CreateShortcut($paths.Link)
    if ($shortcut.TargetPath -ne $exe -or $shortcut.Arguments -ne '' -or $shortcut.WorkingDirectory -ne (Join-Path $paths.App 'bin')) { throw 'Start menu registration belongs to another application; preserved.' }
}
# All ownership checks complete before mutating either registration.
if (Test-Path -LiteralPath $paths.Registry) { Remove-Item -LiteralPath $paths.Registry -Recurse }
if (Test-Path -LiteralPath $paths.Link) { Remove-Item -LiteralPath $paths.Link }
Remove-Item -LiteralPath $paths.App -Recurse -Force
Write-Host 'Trove application removed. Workspaces, model caches, credentials and settings preserved.'
