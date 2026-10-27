# Real MSI transactions, restricted to an ephemeral GitHub-hosted Windows runner.
# Never run this on the owner's computer. The Companion executable is never launched.
[CmdletBinding()]
param(
    [string]$OutDir = (Join-Path $PSScriptRoot '..\..\dist'),
    [switch]$Worker
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
function Assert($condition, [string]$message) { if (!$condition) { throw $message } }
if (!$Worker) {
    Assert ($env:GITHUB_ACTIONS -eq 'true' -and $env:RUNNER_ENVIRONMENT -eq 'github-hosted') 'Real installation tests require an ephemeral GitHub-hosted runner.'
    $account = 'FPInstallerTest'
    $folder = Join-Path $env:PUBLIC ('ForeverPulseInstallerTest-' + [guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $folder | Out-Null
    $password = ConvertTo-SecureString (([guid]::NewGuid().ToString('N')) + 'aA1!') -AsPlainText -Force
    New-LocalUser -Name $account -Password $password -Description 'Ephemeral MSI regression test' | Out-Null
    try {
        Add-LocalGroupMember -SID 'S-1-5-32-545' -Member $account
        $acl = Get-Acl -LiteralPath $folder
        $acl.AddAccessRule([Security.AccessControl.FileSystemAccessRule]::new($account, 'FullControl', 'ContainerInherit,ObjectInherit', 'None', 'Allow'))
        Set-Acl -LiteralPath $folder -AclObject $acl
        Copy-Item -LiteralPath (Join-Path $OutDir 'ForeverPulseCompanion.msi') -Destination (Join-Path $folder 'new.msi')
        Copy-Item -LiteralPath (Join-Path $OutDir 'ForeverPulseCompanion.exe') -Destination (Join-Path $folder 'expected.exe')
        Copy-Item -LiteralPath $PSCommandPath -Destination (Join-Path $folder 'test.ps1')
        Invoke-WebRequest -Uri 'https://github.com/piconguillaume1417a-gif/forever-pulse-companion/releases/download/v0.10.0-rc.6/ForeverPulseCompanion.msi' -OutFile (Join-Path $folder 'previous.msi')
        Assert ((Get-FileHash (Join-Path $folder 'previous.msi')).Hash -eq 'A47AF4707782D9CAC4AD1CD126FEC6B13EC58231A0270AE6D8CE1DA5DFA76610') 'Previous release fixture changed.'
        $credential = [pscredential]::new("$env:COMPUTERNAME\$account", $password)
        $process = Start-Process -FilePath "$env:WINDIR\System32\WindowsPowerShell\v1.0\powershell.exe" -Credential $credential -LoadUserProfile -WindowStyle Hidden -Wait -PassThru -ArgumentList ('-NoProfile -File "' + (Join-Path $folder 'test.ps1') + '" -Worker -OutDir "' + $folder + '"')
        Get-Content -LiteralPath (Join-Path $folder 'result.txt')
        Assert ($process.ExitCode -eq 0) "Real MSI lifecycle test failed: $($process.ExitCode)"
    } finally {
        # Preserve bounded diagnostics as a workflow artifact, even on failure.
        $reports = Join-Path $OutDir 'installer-lifecycle'
        New-Item -ItemType Directory -Force -Path $reports | Out-Null
        Get-ChildItem -LiteralPath $folder -File | Where-Object { $_.Extension -in @('.log','.txt') } | Copy-Item -Destination $reports
        Remove-LocalUser -Name $account
    }
    return
}

$report = Join-Path $OutDir 'result.txt'
try {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = [Security.Principal.WindowsPrincipal]::new($identity)
    Assert ($identity.Name.EndsWith('\FPInstallerTest')) 'Worker must run as the dedicated test user.'
    Assert (!$principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) 'Worker must not be elevated.'
    $profile = [Environment]::GetFolderPath('UserProfile')
    Assert ((Split-Path $profile -Leaf) -like 'FPInstallerTest*') 'Unexpected profile.'
    $local = [Environment]::GetFolderPath('LocalApplicationData')
    $roaming = [Environment]::GetFolderPath('ApplicationData')
    Assert ($local.StartsWith($profile + '\') -and $roaming.StartsWith($profile + '\')) 'Unexpected application data paths.'
    Add-Type @'
using System.Runtime.InteropServices;
public static class MsiState {
    [DllImport("msi.dll", CharSet=CharSet.Unicode)]
    public static extern int MsiQueryProductState(string product);
}
'@
    function ProductCode([string]$path) {
        $installer = New-Object -ComObject WindowsInstaller.Installer
        $database = $installer.OpenDatabase($path, 0)
        $view = $database.OpenView('SELECT `Value` FROM `Property` WHERE `Property` = ''ProductCode''')
        $view.Execute()
        $record = $view.Fetch()
        try { return $record.GetType().InvokeMember('StringData', [Reflection.BindingFlags]::GetProperty, $null, $record, @([int]1)) }
        finally {
            $view.Close()
            foreach ($item in @($record,$view,$database,$installer)) { [Runtime.InteropServices.Marshal]::FinalReleaseComObject($item) | Out-Null }
        }
    }
    function RunMsi([string]$arguments, [string]$name) {
        $log = Join-Path $OutDir ($name + '.log')
        $process = Start-Process -FilePath "$env:WINDIR\System32\msiexec.exe" -ArgumentList ($arguments + ' /qn /norestart ALLUSERS="" /L*v "' + $log + '"') -WindowStyle Hidden -Wait -PassThru
        Assert ($process.ExitCode -eq 0) "$name failed: $($process.ExitCode)"
        Add-Content -LiteralPath $report -Value ("PASS: $name (non-admin, per-user)")
    }
    $previous = Join-Path $OutDir 'previous.msi'
    $current = Join-Path $OutDir 'new.msi'
    $oldCode = ProductCode $previous
    $newCode = ProductCode $current
    Assert ($oldCode -ne $newCode) 'A new installer release must have a new ProductCode.'
    RunMsi ('/i "' + $previous + '" DESKTOPSHORTCUT=1') 'install-rc6'
    Assert ([MsiState]::MsiQueryProductState($oldCode) -eq 5) 'rc.6 is not registered.'
    $data = Join-Path $roaming 'ForeverPulse\Companion'
    New-Item -ItemType Directory -Force -Path $data | Out-Null
    $marker = Join-Path $data 'lifecycle-test.txt'
    [IO.File]::WriteAllText($marker, 'synthetic user data preserved')
    $installed = Join-Path $local 'Programs\ForeverPulseCompanion\ForeverPulseCompanion.exe'
    RunMsi ('/i "' + $current + '" DESKTOPSHORTCUT=1') 'upgrade'
    Assert ([MsiState]::MsiQueryProductState($oldCode) -ne 5) 'Old product still installed.'
    Assert ([MsiState]::MsiQueryProductState($newCode) -eq 5) 'New product is not registered.'
    Assert ((Get-FileHash $installed).Hash -eq (Get-FileHash (Join-Path $OutDir 'expected.exe')).Hash) 'Upgrade installed the wrong binary.'
    # The real wizard extracts its package into a fresh folder on every attempt.
    $repair = Join-Path $OutDir 'second-launch'
    New-Item -ItemType Directory -Path $repair | Out-Null
    $repairMsi = Join-Path $repair 'companion.msi'
    Copy-Item -LiteralPath $current -Destination $repairMsi
    RunMsi ('/i "' + $repairMsi + '" DESKTOPSHORTCUT=1 REINSTALL=ALL REINSTALLMODE=vomus') 'repair-same-package-new-path'
    Assert ((Get-FileHash $installed).Hash -eq (Get-FileHash (Join-Path $OutDir 'expected.exe')).Hash) 'Repair changed the binary.'
    RunMsi ('/x ' + $newCode) 'uninstall'
    Assert ([MsiState]::MsiQueryProductState($newCode) -ne 5) 'New product still installed.'
    Assert (!(Test-Path -LiteralPath $installed)) 'Uninstall left the Companion executable.'
    Assert ([IO.File]::ReadAllText($marker) -eq 'synthetic user data preserved') 'User data was changed.'
    Add-Content -LiteralPath $report -Value 'PASS: user data preserved; no Companion process launched.'
    exit 0
} catch {
    Add-Content -LiteralPath $report -Value $_.Exception.ToString()
    exit 1
}
