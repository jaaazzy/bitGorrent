$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes
Add-Type @'
using System;
using System.Text;
using System.Runtime.InteropServices;
public static class NativeSmokeWindows {
    public delegate bool Callback(IntPtr handle, IntPtr parameter);
    [DllImport("user32.dll")] public static extern bool EnumWindows(Callback callback, IntPtr parameter);
    [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr handle, out uint pid);
    [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern int GetWindowText(IntPtr handle, StringBuilder title, int count);
}
'@

$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '../..')).Path
$binary = Join-Path $projectRoot 'build/bin/bitGorrent.exe'
$temporaryRoot = [IO.Path]::GetFullPath([IO.Path]::GetTempPath())
$profilePath = Join-Path $temporaryRoot ('bitgorrent-smoke-' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $profilePath | Out-Null
$originalAppData = $env:APPDATA
$originalConfig = $env:BITGORRENT_CONFIG_DIR
$nativeProcess = $null
try {
    $env:APPDATA = $profilePath
    $env:BITGORRENT_CONFIG_DIR = $profilePath
    $nativeProcess = Start-Process -FilePath $binary -WindowStyle Hidden -PassThru
    $env:APPDATA = $originalAppData
    $env:BITGORRENT_CONFIG_DIR = $originalConfig
    $deadline = [DateTime]::UtcNow.AddSeconds(25)
    $found = $false
    while ([DateTime]::UtcNow -lt $deadline) {
        $nativeProcess.Refresh()
        if ($nativeProcess.HasExited) { throw "Packaged app exited with code $($nativeProcess.ExitCode)" }
        $script:windowHandle = [IntPtr]::Zero
        [NativeSmokeWindows]::EnumWindows({ param($handle, $parameter)
            [uint32]$windowProcess = 0
            [NativeSmokeWindows]::GetWindowThreadProcessId($handle, [ref]$windowProcess) | Out-Null
            if ($windowProcess -eq $nativeProcess.Id) {
                $title = New-Object Text.StringBuilder 256
                [NativeSmokeWindows]::GetWindowText($handle, $title, 256) | Out-Null
                if ($title.ToString() -eq 'bitGorrent') { $script:windowHandle = $handle }
            }
            return $true
        }, [IntPtr]::Zero) | Out-Null
        if ($script:windowHandle -ne [IntPtr]::Zero) {
            $root = [System.Windows.Automation.AutomationElement]::FromHandle($script:windowHandle)
            if ($null -ne $root) {
                $condition = New-Object System.Windows.Automation.PropertyCondition([System.Windows.Automation.AutomationElement]::NameProperty, 'Your next download starts here')
                $element = $root.FindFirst([System.Windows.Automation.TreeScope]::Descendants, $condition)
                if ($null -ne $element) { $found = $true; break }
            }
        }
        Start-Sleep -Milliseconds 250
    }
    if (-not $found) {
        if ($null -ne $root) { $root.FindAll([System.Windows.Automation.TreeScope]::Descendants, [System.Windows.Automation.Condition]::TrueCondition) | ForEach-Object { $_.Current.Name } | Select-Object -First 20 | Write-Output }
        throw 'The packaged app did not expose its rendered empty state through Windows UI Automation.'
    }
    Write-Output 'PASS: packaged Windows app rendered its frontend with the real Wails host.'
} finally {
    $env:APPDATA = $originalAppData
    $env:BITGORRENT_CONFIG_DIR = $originalConfig
    if ($null -ne $nativeProcess -and -not $nativeProcess.HasExited) {
        $nativeProcess.CloseMainWindow() | Out-Null
        if (-not $nativeProcess.WaitForExit(5000)) { $nativeProcess.Kill(); $nativeProcess.WaitForExit(5000) | Out-Null }
    }
    $resolvedProfile = [IO.Path]::GetFullPath($profilePath)
    if ($resolvedProfile.StartsWith($temporaryRoot, [StringComparison]::OrdinalIgnoreCase) -and (Split-Path $resolvedProfile -Leaf).StartsWith('bitgorrent-smoke-')) {
        Remove-Item -LiteralPath $resolvedProfile -Recurse -Force -ErrorAction SilentlyContinue
    }
}
