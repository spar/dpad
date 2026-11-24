# Install the latest published dpad release for the current Windows user.
& {
    $ErrorActionPreference = 'Stop'
    $cpu = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
    $arch = switch ($cpu) {
        'AMD64' { 'amd64' }
        'ARM64' { 'arm64' }
        default { throw "Unsupported CPU architecture: $cpu" }
    }
    $asset = "dpad-windows-$arch.zip"
    $url = 'https://github.com/spar/dpad/releases/latest/download'
    $tmp = Join-Path ([IO.Path]::GetTempPath()) ([IO.Path]::GetRandomFileName())
    New-Item -ItemType Directory $tmp | Out-Null
    $zip = Join-Path $tmp $asset
    try {
        Invoke-WebRequest -UseBasicParsing "$url/$asset" -OutFile $zip
        $shaFile = (Invoke-WebRequest -UseBasicParsing "$url/SHA256SUMS").Content
        if ($shaFile -is [byte[]]) { $shaFile = [Text.Encoding]::UTF8.GetString($shaFile) }
        $pattern = '(?m)^([0-9a-fA-F]{64})  ' + [regex]::Escape($asset) + '\r?$'
        if ($shaFile -notmatch $pattern) { throw "Missing checksum for $asset" }
        if ((Get-FileHash -Algorithm SHA256 $zip).Hash -ne $Matches[1]) { throw "Checksum mismatch for $asset" }

        Expand-Archive -Path $zip -DestinationPath $tmp
        $exe = Join-Path $tmp 'dpad.exe'

        $bin = Join-Path $env:LOCALAPPDATA 'Programs\dpad'
        New-Item -ItemType Directory -Force $bin | Out-Null
        Copy-Item $exe (Join-Path $bin 'dpad.exe') -Force
        Copy-Item $exe (Join-Path $bin 'dp.exe') -Force
        $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
        if (($userPath -split ';') -notcontains $bin) {
            [Environment]::SetEnvironmentVariable('Path', (($userPath, $bin | Where-Object { $_ }) -join ';'), 'User')
        }
        if (($env:Path -split ';') -notcontains $bin) { $env:Path += ";$bin" }
        & (Join-Path $bin 'dpad.exe') --version
    } finally {
        Remove-Item $tmp -Recurse -Force
    }
}
