# Go's module scanner skips OneDrive cloud reparse-point source files.
# Replace readable cloud files with byte-identical ordinary local files.
[CmdletBinding()]
param()
$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$sources = @('app.go', 'main.go') | ForEach-Object { Get-Item -LiteralPath (Join-Path $projectRoot $_) }
$sources += @(Get-ChildItem -LiteralPath (Join-Path $projectRoot 'internal') -Recurse -Filter '*.go' -File)
$repaired = 0
foreach ($source in $sources) {
    if (($source.Attributes -band [IO.FileAttributes]::ReparsePoint) -eq 0 -or $source.LinkType) { continue }
    $temporary = Join-Path $source.DirectoryName ([IO.Path]::GetRandomFileName())
    try {
        $content = [IO.File]::ReadAllBytes($source.FullName)
        [IO.File]::WriteAllBytes($temporary, $content)
        Move-Item -LiteralPath $temporary -Destination $source.FullName -Force
        $repaired++
    } finally {
        if (Test-Path -LiteralPath $temporary) { Remove-Item -LiteralPath $temporary -Force }
    }
}
Write-Output "Made $repaired Go source file(s) ordinary local files; source contents are unchanged."
Push-Location -LiteralPath $projectRoot
try {
    go mod tidy
    if ($LASTEXITCODE -ne 0) { throw 'go mod tidy failed after repairing local source files.' }
} finally {
    Pop-Location
}
