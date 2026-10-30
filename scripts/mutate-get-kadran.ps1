# Checks that scripts/check-get-kadran.ps1 really guards scripts/get-kadran.ps1
# (K-159). A mutant run by hand guards nothing: this one runs in CI on Windows.
#
# Each mutant must match exactly once (K-127) and the baseline must be green.
# The script is ASCII: Windows PowerShell 5.1 reads UTF-8 without a BOM as ANSI.
$ErrorActionPreference = 'Continue'

$G = Join-Path $PSScriptRoot 'get-kadran.ps1'
$C = Join-Path $PSScriptRoot 'check-get-kadran.ps1'
$exe = (Get-Process -Id $PID).Path
$orj = [IO.File]::ReadAllText($G)
$script:fail = $false

function Olc {
    & $exe -NoProfile -ExecutionPolicy Bypass -File $C *> $null
    return $LASTEXITCODE -eq 0
}

function Mutate([string]$ad, [string]$a, [string]$b) {
    $n = ([regex]::Matches($orj, [regex]::Escape($a))).Count
    if ($n -ne 1) {
        Write-Output "  !! COULD NOT APPLY ($n matches): $ad"
        $script:fail = $true
        return
    }
    [IO.File]::WriteAllText($G, $orj.Replace($a, $b))
    # A mutant that does not parse fails every check: a fake "caught".
    $hatalar = $null
    [void][Management.Automation.Language.Parser]::ParseFile($G, [ref]$null, [ref]$hatalar)
    if ($hatalar.Count -ne 0) {
        Write-Output "  !! DOES NOT PARSE: $ad"
        $script:fail = $true
        return
    }
    if (Olc) {
        Write-Output "  NOT CAUGHT: $ad"
        $script:fail = $true
    } else {
        Write-Output "  caught: $ad"
    }
}

if (-not (Olc)) {
    Write-Output '!! BASELINE RED: the check fails on the real script, nothing was measured'
    exit 1
}

try {
    Mutate 'a checksum difference is ignored' '(-not $want) -or ($got -ne $want)' '(-not $want)'
    Mutate 'a binary missing from SHA256SUMS passes' '(-not $want) -or ($got -ne $want)' '$want -and ($got -ne $want)'
    Mutate 'the *name form is not read' '\s+\*?(.+?)' '\s+(.+?)'
    Mutate 'exit closes the session under iex' 'if ($PSCommandPath) { exit $code }' 'exit $code'
    Mutate 'the user Path is read expanded' '$k.GetValue(''Path'', '''', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)' '$k.GetValue(''Path'', '''')'
    Mutate 'the user Path loses its kind' '$k.SetValue(''Path'', $new, $kind)' '$k.SetValue(''Path'', $new)'
    Mutate 'the directory is added to the Path twice' '-ieq $dir.TrimEnd' '-ieq "none" + $dir.TrimEnd'
    Mutate 'a locked file gets no clear message' 'throw ''kadran.exe is in use; close it and run again''' 'throw'
    Mutate 'the architecture is not checked' 'if ($arch -ne ''AMD64'')' 'if ($false)'
    Mutate 'KADRAN_VERSION is not validated' 'elseif (-not (Test-KadranVersion $ver))' 'elseif ($false)'
    Mutate 'the API tag is not validated' 'if (-not (Test-KadranVersion $v))' 'if ($false)'
    Mutate 'KADRAN_INSTALL_DIR changes the Path too' "if (`$custom) {`n" "if (`$false) {`n"
} finally {
    [IO.File]::WriteAllText($G, $orj)
}

Write-Output ''
if ($script:fail) {
    Write-Output 'At least one mutant was not caught.'
    exit 1
}
Write-Output 'All mutants caught.'
exit 0
