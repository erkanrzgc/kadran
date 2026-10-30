# Runs scripts/get-kadran.ps1 against a fake release, in child processes of the
# same PowerShell (K-159). The file is ASCII on purpose: Windows PowerShell 5.1
# reads UTF-8 without a BOM as ANSI.
#
#   pwsh -File scripts/check-get-kadran.ps1
#   powershell -ExecutionPolicy Bypass -File scripts/check-get-kadran.ps1
$ErrorActionPreference = 'Continue'

$script:fail = $false
$exe = (Get-Process -Id $PID).Path
$getKadran = Join-Path $PSScriptRoot 'get-kadran.ps1'
$T = Join-Path ([IO.Path]::GetTempPath()) ('kadran-check-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $T | Out-Null

function Dene([string]$ad, [scriptblock]$kod) {
    $ok = $false
    $script:son = ''
    try { $ok = [bool](& $kod) } catch { $ok = $false; $script:son += "$_" }
    if ($ok) {
        Write-Output "  [ok] $ad"
    } else {
        Write-Output "  [XX] $ad"
        Write-Output "       last output: $($script:son)"
        $script:fail = $true
    }
}

function Ozet([string]$metin) {
    $sha = [Security.Cryptography.SHA256]::Create()
    $b = $sha.ComputeHash([Text.Encoding]::ASCII.GetBytes($metin))
    return (($b | ForEach-Object { $_.ToString('x2') }) -join '')
}

$B = 'kadran-v9.9.9-windows-amd64.exe'

# Yayin <root> <binary content> <SHA256SUMS line>
function Yayin([string]$kok, [string]$icerik, [string]$satir) {
    $d = Join-Path $kok 'v9.9.9'
    New-Item -ItemType Directory -Force -Path $d | Out-Null
    [IO.File]::WriteAllText((Join-Path $d $B), $icerik)
    [IO.File]::WriteAllText((Join-Path $d 'SHA256SUMS'), "$satir`n")
}
Yayin "$T\iyi" 'binary-v9' "$(Ozet 'binary-v9')  $B"
Yayin "$T\yildiz" 'binary-v9' "$(Ozet 'binary-v9') *$B"
Yayin "$T\sahte" 'evil' "$(Ozet 'binary-v9')  $B"
Yayin "$T\eksik" 'binary-v9' "$(Ozet 'binary-v9')  kadran-v9.9.9-linux-amd64"

# Env <hashtable> <scriptblock>: runs the block with the variables set, then
# puts the old values back. [NullString] removes a variable: PowerShell turns a
# plain $null into "", and newer .NET keeps that as an empty variable.
function SetEnv([string]$k, $v) {
    if ($null -eq $v) { $v = [NullString]::Value }
    [Environment]::SetEnvironmentVariable($k, $v)
}
function Env([hashtable]$vars, [scriptblock]$kod) {
    $eski = @{}
    foreach ($k in $vars.Keys) {
        $eski[$k] = [Environment]::GetEnvironmentVariable($k)
        SetEnv $k $vars[$k]
    }
    try { return & $kod } finally {
        foreach ($k in $eski.Keys) { SetEnv $k $eski[$k] }
    }
}

# Kur <release root> <install dir> [extra env]: output and exit code of a child.
function Kur([string]$kok, [string]$dizin, [hashtable]$ek = @{}) {
    $vars = @{ KADRAN_RELEASE_BASE = $kok; KADRAN_VERSION = 'v9.9.9'; KADRAN_INSTALL_DIR = $dizin }
    foreach ($k in $ek.Keys) { $vars[$k] = $ek[$k] }
    return Env $vars {
        $o = (& $exe -NoProfile -ExecutionPolicy Bypass -File $getKadran 2>&1 | Out-String -Width 4096)
        $script:son = $o
        [pscustomobject]@{ Kod = $LASTEXITCODE; Cikti = $o }
    }
}

function Icerik([string]$yol) { return [IO.File]::ReadAllText($yol) }

function GeciciYok([string]$dizin) {
    return @(Get-ChildItem -LiteralPath $dizin -Force | Where-Object { $_.Name -like '*.new*' }).Count -eq 0
}

function Reddedilir($sonuc, [string]$ileti, [string]$dizin) {
    return ($sonuc.Kod -ne 0) -and $sonuc.Cikti.Contains($ileti) -and -not (Test-Path (Join-Path $dizin 'kadran.exe'))
}

Write-Output '== Install =='

Dene 'installs: same content' {
    $r = Kur "$T\iyi" "$T\b1"
    ($r.Kod -eq 0) -and ((Icerik "$T\b1\kadran.exe") -ceq 'binary-v9') -and $r.Cikti.Contains("installed kadran v9.9.9 to $T\b1\kadran.exe") -and $r.Cikti.Contains("add $T\b1 to your PATH")
}
Dene 'reads the *name form in SHA256SUMS' {
    $r = Kur "$T\yildiz" "$T\b1y"
    ($r.Kod -eq 0) -and ((Icerik "$T\b1y\kadran.exe") -ceq 'binary-v9')
}
Dene 'replaces an old version, leaves no temporary file' {
    New-Item -ItemType Directory -Path "$T\b1e" | Out-Null
    [IO.File]::WriteAllText("$T\b1e\kadran.exe", 'old')
    $r = Kur "$T\iyi" "$T\b1e"
    ($r.Kod -eq 0) -and ((Icerik "$T\b1e\kadran.exe") -ceq 'binary-v9') -and (GeciciYok "$T\b1e")
}

Write-Output '== Checksum =='

Dene 'a tampered binary is not installed' {
    Reddedilir (Kur "$T\sahte" "$T\b2") "checksum mismatch for $B; nothing installed" "$T\b2"
}
Dene 'a binary missing from SHA256SUMS is not installed' {
    Reddedilir (Kur "$T\eksik" "$T\b3") "checksum mismatch for $B; nothing installed" "$T\b3"
}
Dene 'CONTROL: an unverified copy would have installed the tampered file' {
    $h = (Get-FileHash -Algorithm SHA256 -LiteralPath "$T\sahte\v9.9.9\$B").Hash
    ((Icerik "$T\sahte\v9.9.9\$B") -ceq 'evil') -and ($h -ne (Ozet 'binary-v9'))
}

Write-Output '== Replacing a file in use =='

Dene 'a locked kadran.exe stays intact, the message says so' {
    New-Item -ItemType Directory -Path "$T\b4" | Out-Null
    [IO.File]::WriteAllText("$T\b4\kadran.exe", 'old')
    $tut = [IO.File]::Open("$T\b4\kadran.exe", 'Open', 'Read', 'None')
    try { $r = Kur "$T\iyi" "$T\b4" } finally { $tut.Dispose() }
    ($r.Kod -ne 0) -and $r.Cikti.Contains('kadran.exe is in use; close it and run again') -and
        ((Icerik "$T\b4\kadran.exe") -ceq 'old') -and (GeciciYok "$T\b4")
}

Write-Output '== Platform and version =='

Dene 'unsupported architecture' {
    Reddedilir (Kur "$T\iyi" "$T\b5" @{ PROCESSOR_ARCHITECTURE = 'ARM64'; PROCESSOR_ARCHITEW6432 = $null }) 'unsupported platform: windows/ARM64' "$T\b5"
}
Dene 'a malformed KADRAN_VERSION is refused' {
    Reddedilir (Kur "$T\iyi" "$T\b6" @{ KADRAN_VERSION = 'v1\..\..\x' }) 'invalid KADRAN_VERSION' "$T\b6"
}

# SonSurum <API response> <install dir>: KADRAN_VERSION empty, API from a file.
function SonSurum([string]$json, [string]$dizin) {
    [IO.File]::WriteAllText("$T\latest.json", $json)
    return Kur "$T\iyi" $dizin @{ KADRAN_VERSION = $null; KADRAN_RELEASE_API = "$T\latest.json" }
}
Dene 'reads the latest version from the API' {
    $r = SonSurum '{"url":"x","tag_name":"v9.9.9","name":"Kadran v9.9.9"}' "$T\b7"
    ($r.Kod -eq 0) -and ((Icerik "$T\b7\kadran.exe") -ceq 'binary-v9')
}
Dene 'a malformed tag from the API is refused' {
    Reddedilir (SonSurum '{"tag_name":"v1;rm x"}' "$T\b8") 'could not determine the latest version' "$T\b8"
}
Dene 'a response with no tag is refused' {
    Reddedilir (SonSurum '{"message":"rate limited"}' "$T\b9") 'could not determine the latest version' "$T\b9"
}

Write-Output '== irm | iex =='

# OturumdaKur <install dir>: a failing install inside a session, the way the README runs it.
function OturumdaKur([string]$dizin) {
    $vars = @{ KADRAN_RELEASE_BASE = "$T\sahte"; KADRAN_VERSION = 'v9.9.9'; KADRAN_INSTALL_DIR = $dizin; KADRAN_SCRIPT = $getKadran }
    return Env $vars {
        & $exe -NoProfile -ExecutionPolicy Bypass -Command 'Get-Content -Raw -LiteralPath $env:KADRAN_SCRIPT | Invoke-Expression; Write-Output "ALIVE EAP=$ErrorActionPreference FN=$(@(Get-Command Get-KadranFile -ErrorAction SilentlyContinue).Count)"' 2>&1 | Out-String -Width 4096
    }
}
Dene 'a failure does not close the session or leak settings and functions' {
    $o = OturumdaKur "$T\b10"
    $script:son = $o
    # Each on its own: Windows PowerShell 5.1 passes the inner quotes differently.
    $o.Contains('checksum mismatch') -and $o.Contains('ALIVE') -and $o.Contains('EAP=Continue') -and $o.Contains('FN=0') -and
        -not (Test-Path "$T\b10\kadran.exe")
}

Write-Output '== User Path =='

# Real registry shape, under a test key: the script never touches HKCU:\Environment here.
$anahtar = 'HKCU:\Software\KadranCheckPath' + [guid]::NewGuid().ToString('N')
$hamAnahtar = $anahtar -replace '^HKCU:\\', ''
function YolKur([string]$appdata) {
    return Env @{ LOCALAPPDATA = $appdata; KADRAN_INSTALL_DIR = $null; KADRAN_RELEASE_BASE = "$T\iyi"; KADRAN_VERSION = 'v9.9.9'; KADRAN_PATH_KEY = $anahtar } {
        $o = (& $exe -NoProfile -ExecutionPolicy Bypass -File $getKadran 2>&1 | Out-String -Width 4096)
        $script:son = $o
        [pscustomobject]@{ Kod = $LASTEXITCODE; Cikti = $o }
    }
}
function HamYol {
    $k = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey($hamAnahtar)
    try {
        return [pscustomobject]@{
            Deger = $k.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
            Tur   = $k.GetValueKind('Path')
        }
    } finally { $k.Dispose() }
}
try {
    $k = [Microsoft.Win32.Registry]::CurrentUser.CreateSubKey($hamAnahtar)
    $k.SetValue('Path', 'C:\foo;%USERPROFILE%\bin', [Microsoft.Win32.RegistryValueKind]::ExpandString)
    $k.Dispose()
    $hedef = "$T\app\Programs\kadran"

    Dene 'adds the default directory, keeps %VAR% entries unexpanded' {
        $r = YolKur "$T\app"
        $y = HamYol
        ($r.Kod -eq 0) -and ((Icerik "$hedef\kadran.exe") -ceq 'binary-v9') -and
            ($y.Deger -ceq "C:\foo;%USERPROFILE%\bin;$hedef") -and ($y.Tur -eq 'ExpandString') -and
            $r.Cikti.Contains("added $hedef to your user Path")
    }
    Dene 'a second run does not add it again and stays quiet' {
        $r = YolKur "$T\app"
        ($r.Kod -eq 0) -and ((HamYol).Deger -ceq "C:\foo;%USERPROFILE%\bin;$hedef") -and -not $r.Cikti.Contains('to your user Path')
    }
    Dene 'KADRAN_INSTALL_DIR leaves the Path alone' {
        $once = (HamYol).Deger
        $r = Kur "$T\iyi" "$T\b11" @{ KADRAN_PATH_KEY = $anahtar }
        ($r.Kod -eq 0) -and ((HamYol).Deger -ceq $once)
    }
    Dene 'creates the value when the user has no Path' {
        $k = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey($hamAnahtar, $true)
        $k.DeleteValue('Path')
        $k.Dispose()
        $r = YolKur "$T\app"
        ($r.Kod -eq 0) -and ((HamYol).Deger -ceq $hedef)
    }
} finally {
    [Microsoft.Win32.Registry]::CurrentUser.DeleteSubKeyTree($hamAnahtar, $false)
}

Remove-Item -LiteralPath $T -Recurse -Force -ErrorAction SilentlyContinue

Write-Output ''
if ($script:fail) {
    Write-Output 'get-kadran.ps1 check RED.'
    exit 1
}
Write-Output 'get-kadran.ps1 check green.'
exit 0
