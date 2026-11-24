# Install the latest published dpad release for the current Windows user.
& {
    $ErrorActionPreference = 'Stop'
    $cpu = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
    $arch = switch ($cpu) {
        'AMD64' { 'amd64' }
        'ARM64' { 'arm64' }
        default { throw "Unsupported CPU architecture: $cpu" }
    }
    $asset = "dpad-windows-$arch.exe"
    $url = 'https://github.com/spar/dpad/releases/latest/download'
    $tmp = [IO.Path]::GetTempFileName()
    try {
        Invoke-WebRequest -UseBasicParsing "$url/$asset" -OutFile $tmp
        $shaFile = (Invoke-WebRequest -UseBasicParsing "$url/SHA256SUMS").Content
        if ($shaFile -is [byte[]]) { $shaFile = [Text.Encoding]::UTF8.GetString($shaFile) }
        $pattern = '(?m)^([0-9a-fA-F]{64})  ' + [regex]::Escape($asset) + '\r?$'
        if ($shaFile -notmatch $pattern) { throw "Missing checksum for $asset" }
        if ((Get-FileHash -Algorithm SHA256 $tmp).Hash -ne $Matches[1]) { throw "Checksum mismatch for $asset" }

        $bin = Join-Path $env:LOCALAPPDATA 'Programs\dpad'
        New-Item -ItemType Directory -Force $bin | Out-Null
        Copy-Item $tmp (Join-Path $bin 'dpad.exe') -Force
        Copy-Item $tmp (Join-Path $bin 'dp.exe') -Force
        $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
        if (($userPath -split ';') -notcontains $bin) {
            [Environment]::SetEnvironmentVariable('Path', (($userPath, $bin | Where-Object { $_ }) -join ';'), 'User')
        }
        if (($env:Path -split ';') -notcontains $bin) { $env:Path += ";$bin" }
        & (Join-Path $bin 'dpad.exe') --version
    } finally {
        Remove-Item $tmp -Force
    }
}
