# Installs the kadran client from a GitHub release (K-159). No administrator rights.
#
#   irm https://raw.githubusercontent.com/erkanrzgc/kadran/main/scripts/get-kadran.ps1 | iex
#
# Environment: KADRAN_VERSION (default: latest release), KADRAN_INSTALL_DIR
# (default: %LOCALAPPDATA%\Programs\kadran), KADRAN_RELEASE_BASE and
# KADRAN_RELEASE_API (a URL or a local path, for mirrors and tests),
# KADRAN_PATH_KEY (the registry key holding the user Path, for tests).
#
# Checks against SHA256SUMS of the same release: that catches a corrupted
# download, not a forged one. The server bundle's hash is built into this
# binary and bootstrap checks it (internal/bootstrap/release.go).
#
# This file stays ASCII: Windows PowerShell 5.1 reads UTF-8 without a BOM as
# ANSI. Everything runs inside one script block: a truncated `irm | iex` does
# not parse, so it never runs half a script, and nothing leaks into the session.
& {
    $ErrorActionPreference = 'Stop'
    $ProgressPreference = 'SilentlyContinue'

    function Test-KadranVersion([string]$v) {
        return $v -cmatch '^v[0-9][A-Za-z0-9.+_-]*\z'
    }

    function Test-KadranUrl([string]$s) {
        return $s -match '^https?://'
    }

    function Get-KadranFile([string]$base, [string]$ver, [string]$name, [string]$dst) {
        try {
            if (Test-KadranUrl $base) {
                Invoke-WebRequest -UseBasicParsing -Uri "$base/$ver/$name" -OutFile $dst
            } else {
                Copy-Item -LiteralPath (Join-Path (Join-Path $base $ver) $name) -Destination $dst
            }
        } catch {
            throw "could not download $base/$ver/$name`: $($_.Exception.Message)"
        }
    }

    function Get-KadranLatest {
        $api = $env:KADRAN_RELEASE_API
        if (-not $api) { $api = 'https://api.github.com/repos/erkanrzgc/kadran/releases/latest' }
        $v = ''
        try {
            if (Test-KadranUrl $api) {
                $j = Invoke-RestMethod -UseBasicParsing -Uri $api
            } else {
                $j = Get-Content -Raw -LiteralPath $api | ConvertFrom-Json
            }
            $v = [string]$j.tag_name
        } catch {
            $v = ''
        }
        if (-not (Test-KadranVersion $v)) {
            throw 'could not determine the latest version; set KADRAN_VERSION (for example v0.9.3)'
        }
        return $v
    }

    # The value goes through the registry, not [Environment]: that API returns
    # %VAR% entries expanded and writes REG_SZ, which would break them for good.
    # Returns $true when it added the directory.
    function Add-KadranUserPath([string]$dir) {
        $keyPath = $env:KADRAN_PATH_KEY
        if (-not $keyPath) { $keyPath = 'HKCU:\Environment' }
        $sub = $keyPath -replace '^HKCU:\\', ''
        $k = [Microsoft.Win32.Registry]::CurrentUser.CreateSubKey($sub)
        try {
            $raw = ''
            $kind = [Microsoft.Win32.RegistryValueKind]::ExpandString
            if ($k.GetValueNames() -contains 'Path') {
                $raw = [string]$k.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
                if ($k.GetValueKind('Path') -eq [Microsoft.Win32.RegistryValueKind]::String) {
                    $kind = [Microsoft.Win32.RegistryValueKind]::String
                }
            }
            foreach ($item in ($raw -split ';')) {
                if (-not $item) { continue }
                if ([Environment]::ExpandEnvironmentVariables($item).TrimEnd('\') -ieq $dir.TrimEnd('\')) {
                    return $false
                }
            }
            $new = if ($raw) { $raw.TrimEnd(';') + ';' + $dir } else { $dir }
            $k.SetValue('Path', $new, $kind)
        } finally {
            $k.Dispose()
        }
        if (-not $env:KADRAN_PATH_KEY) {
            # Tells running programs that the environment changed (WM_SETTINGCHANGE).
            [Environment]::SetEnvironmentVariable('KADRAN_PATH_REFRESH', '1', 'User')
            [Environment]::SetEnvironmentVariable('KADRAN_PATH_REFRESH', $null, 'User')
        }
        return $true
    }

    function Install-Kadran {
        $arch = $env:PROCESSOR_ARCHITEW6432
        if (-not $arch) { $arch = $env:PROCESSOR_ARCHITECTURE }
        if ($arch -ne 'AMD64') { throw "unsupported platform: windows/$arch" }

        # GitHub needs TLS 1.2; Windows PowerShell 5.1 may not offer it by default.
        [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

        $base = $env:KADRAN_RELEASE_BASE
        if (-not $base) { $base = 'https://github.com/erkanrzgc/kadran/releases/download' }
        $base = $base.TrimEnd('/', '\')

        $ver = $env:KADRAN_VERSION
        if (-not $ver) {
            $ver = Get-KadranLatest
        } elseif (-not (Test-KadranVersion $ver)) {
            throw "invalid KADRAN_VERSION: $ver"
        }

        $file = "kadran-$ver-windows-amd64.exe"
        $tmp = Join-Path ([IO.Path]::GetTempPath()) ('kadran-get-' + [guid]::NewGuid().ToString('N'))
        New-Item -ItemType Directory -Path $tmp | Out-Null
        $new = $null
        try {
            $bin = Join-Path $tmp $file
            $sums = Join-Path $tmp 'SHA256SUMS'
            Get-KadranFile $base $ver $file $bin
            Get-KadranFile $base $ver 'SHA256SUMS' $sums

            # sha256sum puts * before the name in binary mode; releases built on Windows have it.
            $want = $null
            foreach ($line in (Get-Content -LiteralPath $sums)) {
                if (($line -match '^([0-9a-fA-F]{64})\s+\*?(.+?)\s*$') -and ($Matches[2] -ceq $file)) {
                    $want = $Matches[1]
                    break
                }
            }
            $got = (Get-FileHash -Algorithm SHA256 -LiteralPath $bin).Hash
            if ((-not $want) -or ($got -ne $want)) {
                throw "checksum mismatch for $file; nothing installed"
            }

            $custom = [bool]$env:KADRAN_INSTALL_DIR
            $dir = if ($custom) { $env:KADRAN_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'Programs\kadran' }
            $dir = (New-Item -ItemType Directory -Force -Path $dir).FullName.TrimEnd('\')
            $target = Join-Path $dir 'kadran.exe'
            if (Test-Path -LiteralPath $target -PathType Container) { throw "$target is a directory" }

            # Temporary name in the same directory, then an atomic replace: a
            # locked or running kadran.exe stays whole.
            $new = "$target.new"
            Copy-Item -LiteralPath $bin -Destination $new -Force
            try {
                if (Test-Path -LiteralPath $target) {
                    # [NullString]: PowerShell turns a plain $null into "" here,
                    # and Replace refuses an empty backup name.
                    [IO.File]::Replace($new, $target, [NullString]::Value)
                } else {
                    [IO.File]::Move($new, $target)
                }
            } catch {
                $cause = $_.Exception.InnerException
                if (($cause -is [IO.IOException]) -or ($cause -is [UnauthorizedAccessException])) {
                    throw 'kadran.exe is in use; close it and run again'
                }
                throw
            }
            $new = $null

            Write-Output "installed kadran $ver to $target"
            if ($custom) {
                if (($env:Path -split ';') -notcontains $dir) { Write-Output "add $dir to your PATH" }
            } else {
                try {
                    if (Add-KadranUserPath $dir) {
                        Write-Output "added $dir to your user Path; open a new terminal to use it"
                    }
                    if (($env:Path -split ';') -notcontains $dir) { $env:Path = "$env:Path;$dir" }
                } catch {
                    Write-Output "could not update your user Path; add $dir to it yourself"
                }
            }
        } finally {
            if ($new -and (Test-Path -LiteralPath $new)) { Remove-Item -LiteralPath $new -Force }
            Remove-Item -LiteralPath $tmp -Recurse -Force
        }
    }

    $code = 0
    try {
        Install-Kadran
    } catch {
        [Console]::Error.WriteLine("get-kadran: $($_.Exception.Message)")
        $code = 1
    }
    # Under `irm | iex` there is no script path, and `exit` would close the
    # user's terminal: the message above is enough there.
    if ($PSCommandPath) { exit $code }
}
