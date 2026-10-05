Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
function Assert-NoReparse([string]$Path) {
    $current = [IO.Path]::GetFullPath($Path)
    while ($current) {
        if (Test-Path -LiteralPath $current) {
            if ((Get-Item -LiteralPath $current -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Reparse paths are not permitted for installation.' }
        }
        $parent = [IO.Path]::GetDirectoryName($current)
        if ($parent -eq $current) { break }
        $current = $parent
    }
}
function Get-TrovePaths {
    $app = Join-Path ([Environment]::GetFolderPath('LocalApplicationData')) 'Programs\Trove'
    $link = Join-Path ([Environment]::GetFolderPath('Programs')) 'Trove.lnk'
    Assert-NoReparse $app
    Assert-NoReparse $link
    return @{ App=$app; Link=$link; Registry='HKCU:\Software\Classes\trove' }
}
function Read-TrovePackage([string]$Root, [bool]$Installed=$false) {
    Assert-NoReparse $Root
    $info = Get-Item -LiteralPath (Join-Path $Root 'desktop-manifest.json')
    if ($info.Length -gt 8388608) { throw 'Package manifest is too large.' }
    $manifest = Get-Content -LiteralPath $info.FullName -Raw | ConvertFrom-Json
    if ($manifest.schemaVersion -ne 1 -or $manifest.appId -ne 'local.trove.desktop' -or $manifest.target -ne 'windows-amd64') { throw 'Wrong desktop package identity.' }
    $expected = New-Object 'System.Collections.Generic.HashSet[string]' ([StringComparer]::OrdinalIgnoreCase)
    foreach ($entry in $manifest.files) {
        $relative = [string]$entry.path
        if ($relative -notmatch '^[A-Za-z0-9_@+.,() /=-]+$' -or $relative.StartsWith('/') -or $relative -match '(^|/)\.\.?(/|$)' -or $relative -match '//|[. ](/|$)' -or -not $expected.Add($relative)) { throw 'Unsafe/duplicate package path.' }
        foreach ($component in $relative.Split('/')) { if ($component.Split('.')[0] -match '^(CON|PRN|AUX|NUL|COM[1-9]|LPT[1-9])$') { throw 'Reserved Windows path.' } }
        $file = Join-Path $Root $relative
        Assert-NoReparse $file
        $stat = Get-Item -LiteralPath $file -Force
        if ($stat.PSIsContainer -or $stat.Length -ne $entry.sizeBytes -or (Get-FileHash -LiteralPath $file -Algorithm SHA256).Hash.ToLowerInvariant() -ne $entry.sha256) { throw "Package bytes differ: $relative" }
    }
    [void]$expected.Add('desktop-manifest.json'); [void]$expected.Add('files.sha256')
    if ($Installed) { [void]$expected.Add('.trove-install.json') }
    foreach ($item in Get-ChildItem -LiteralPath $Root -Recurse -Force) {
        if ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Package contains a reparse entry.' }
        $relative = $item.FullName.Substring($Root.TrimEnd('\').Length+1).Replace('\','/')
        if ($item.PSIsContainer) {
            if (-not ($expected | Where-Object { $_.StartsWith($relative + '/', [StringComparison]::OrdinalIgnoreCase) })) { throw "Unexpected directory preserved: $relative" }
        } else {
            if (-not $expected.Contains($relative)) { throw "Unexpected user file preserved: $relative" }
        }
    }
    return $manifest
}
function Assert-DesktopUser {
    if (-not [Environment]::Is64BitProcess -or $env:PROCESSOR_ARCHITECTURE -ne 'AMD64' -or -not [Environment]::Is64BitOperatingSystem -or [Environment]::OSVersion.Version.Build -lt 22000) { throw 'Requires Windows 11 x64.' }
    $principal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
    if ($principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) { throw 'Run as the installing desktop user, without elevation.' }
}

function Assert-WebView2([string]$Minimum) {
    $guid = '{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}'
    $versions = @()
    foreach ($key in @("HKLM:\SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\$guid", "HKCU:\Software\Microsoft\EdgeUpdate\Clients\$guid")) {
        if (Test-Path -LiteralPath $key) {
            $value = (Get-Item -LiteralPath $key).GetValue('pv')
            if ($value -is [string] -and $value -match '^\d+\.\d+\.\d+\.\d+$') { $versions += [version]$value }
        }
    }
    if (-not ($versions | Where-Object { $_ -ge [version]$Minimum })) { throw 'Install the approved offline x64 WebView2 Evergreen Standalone Runtime first. Trove never downloads a runtime.' }
}
