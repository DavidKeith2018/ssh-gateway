param(
    [Parameter(Mandatory = $true)]
    [string] $InstallerPath
)

$ErrorActionPreference = 'Stop'
$installerFile = (Resolve-Path -LiteralPath $InstallerPath).Path
$installerProcess = Start-Process -FilePath $installerFile -PassThru
try {
    $deadline = (Get-Date).AddSeconds(30)
    $title = ''
    do {
        Start-Sleep -Milliseconds 200
        $installerProcess.Refresh()
        if ($installerProcess.HasExited) {
            throw 'Installer exited before showing its window'
        }
        $title = $installerProcess.MainWindowTitle
    } while ([string]::IsNullOrWhiteSpace($title) -and (Get-Date) -lt $deadline)

    if (-not $title.Contains('SSH Gateway')) {
        throw "Unexpected installer title: $title"
    }
    Write-Output "Installer title verified: $title"
} finally {
    if (-not $installerProcess.HasExited) {
        Stop-Process -Id $installerProcess.Id -Force
        $installerProcess.WaitForExit()
    }
}

# Exercise installation completion without invoking the runtime bootstrapper.
$installDirectory = Join-Path $env:TEMP ("ssh-gateway-install-" + [guid]::NewGuid().ToString('N'))
$setup = $null
try {
    $setup = Start-Process -FilePath $installerFile -ArgumentList "/S /D=$installDirectory" -PassThru
    if (-not $setup.WaitForExit(30000)) {
        throw 'Silent application installation did not finish within 30 seconds'
    }
    if ($setup.ExitCode -ne 0) {
        throw "Application installation failed: $($setup.ExitCode)"
    }
    foreach ($name in @('ssh-gateway-desktop-windows.exe', 'uninstall.exe', 'MicrosoftEdgeWebview2Setup.exe')) {
        if (-not (Test-Path -LiteralPath (Join-Path $installDirectory $name) -PathType Leaf)) {
            throw "Installed file is missing: $name"
        }
    }
    Write-Output 'Application installation completed; the runtime bootstrapper remains available for manual use.'
} finally {
    if ($setup -and -not $setup.HasExited) {
        Stop-Process -Id $setup.Id -Force
        $setup.WaitForExit()
    }
    $uninstaller = Join-Path $installDirectory 'uninstall.exe'
    if (Test-Path -LiteralPath $uninstaller) {
        $removal = Start-Process -FilePath $uninstaller -ArgumentList "/S _?=$installDirectory" -PassThru
        if (-not $removal.WaitForExit(30000)) {
            Stop-Process -Id $removal.Id -Force
            throw 'Test uninstaller did not finish within 30 seconds'
        }
    }
    Remove-Item -LiteralPath $installDirectory -Recurse -Force -ErrorAction SilentlyContinue
}
