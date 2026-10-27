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
    $policy = [Microsoft.Win32.Registry]::LocalMachine.CreateSubKey('Software\Policies\Microsoft\Windows\Installer')
    $previousDisableMsi = $policy.GetValue('DisableMSI', $null)
    try {
        # Windows Server runners default to DisableMSI=1 (reject all unmanaged
        # packages for standard users). Model Windows 11's normal value, 0, only
        # in this disposable fixture. Never change elevation/SecureRepair policy.
        $policy.SetValue('DisableMSI', 0, [Microsoft.Win32.RegistryValueKind]::DWord)
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
        if ($null -eq $previousDisableMsi) { $policy.DeleteValue('DisableMSI', $false) }
        else { $policy.SetValue('DisableMSI', $previousDisableMsi, [Microsoft.Win32.RegistryValueKind]::DWord) }
        $policy.Dispose()
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
    # Resolve expandable known-folder paths only after replacing inherited values.
    $env:USERPROFILE = $profile
    $env:LOCALAPPDATA = Join-Path $profile 'AppData\Local'
    $env:APPDATA = Join-Path $profile 'AppData\Roaming'
    New-Item -ItemType Directory -Force -Path $env:LOCALAPPDATA,$env:APPDATA | Out-Null
    # No Explorer first logon runs on this headless account. Initialize only its
    # ordinary shell folders, as a desktop logon would; no repair security changes.
    $folders = @{
        'Local AppData' = $env:LOCALAPPDATA
        'AppData' = $env:APPDATA
        'Desktop' = (Join-Path $profile 'Desktop')
        'Start Menu' = (Join-Path $env:APPDATA 'Microsoft\Windows\Start Menu')
        'Programs' = (Join-Path $env:APPDATA 'Microsoft\Windows\Start Menu\Programs')
    }
    foreach ($keyName in @('User Shell Folders','Shell Folders')) {
        $key = [Microsoft.Win32.Registry]::CurrentUser.CreateSubKey('Software\Microsoft\Windows\CurrentVersion\Explorer\' + $keyName)
        try {
            foreach ($entry in $folders.GetEnumerator()) {
                New-Item -ItemType Directory -Force -Path $entry.Value | Out-Null
                $key.SetValue($entry.Key, $entry.Value, [Microsoft.Win32.RegistryValueKind]::String)
            }
        } finally { $key.Dispose() }
    }
    $local = $env:LOCALAPPDATA
    $roaming = $env:APPDATA
    # Start-Process -Credential may inherit environment strings from runneradmin.
    # Keep compilers and MSI temporary files inside the actual test user's profile.
    $env:USERPROFILE = $profile
    $env:LOCALAPPDATA = $local
    $env:APPDATA = $roaming
    $env:TEMP = Join-Path $local 'Temp'
    $env:TMP = $env:TEMP
    New-Item -ItemType Directory -Force -Path $env:TEMP | Out-Null
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
    function Registered([string]$code) {
        Assert ($code -match '^\{[0-9A-F-]{36}\}$') "Invalid product identity: $code"
        # Read the registration written by the external msiexec transaction;
        # do not rely on MSI's state cached by the database-reading process.
        $found = Test-Path -LiteralPath ('HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\' + $code)
        Add-Content -LiteralPath $report -Value ("Registration $code : $found; MSI query: " + [MsiState]::MsiQueryProductState($code))
        return $found
    }
    $previous = Join-Path $OutDir 'previous.msi'
    $current = Join-Path $OutDir 'new.msi'
    $oldCode = ProductCode $previous
    $newCode = ProductCode $current
    Assert ($oldCode -ne $newCode) 'A new installer release must have a new ProductCode.'
    RunMsi ('/i "' + $previous + '" DESKTOPSHORTCUT=1') 'install-rc6'
    Assert (Registered $oldCode) 'rc.6 is not registered.'
    $installed = Join-Path $local 'Programs\ForeverPulseCompanion\ForeverPulseCompanion.exe'
    Assert ((Get-FileHash $installed).Hash -eq 'B71FFA9B70DF217E959ED3BB7CB8E493172A131384855BB9D3DE4B485DD749AE') 'rc.6 installed the wrong binary.'
    $data = Join-Path $roaming 'ForeverPulse\Companion'
    New-Item -ItemType Directory -Force -Path $data | Out-Null
    $marker = Join-Path $data 'lifecycle-test.txt'
    [IO.File]::WriteAllText($marker, 'synthetic user data preserved')
    RunMsi ('/i "' + $current + '" DESKTOPSHORTCUT=1') 'upgrade'
    Assert (!(Registered $oldCode)) 'Old product still installed.'
    Assert (Registered $newCode) 'New product is not registered.'
    Assert ((Get-FileHash $installed).Hash -eq (Get-FileHash (Join-Path $OutDir 'expected.exe')).Hash) 'Upgrade installed the wrong binary.'
    # The real wizard extracts its package into a fresh folder on every attempt.
    $repair = Join-Path $OutDir 'second-launch'
    New-Item -ItemType Directory -Path $repair | Out-Null
    $repairMsi = Join-Path $repair 'companion.msi'
    Copy-Item -LiteralPath $current -Destination $repairMsi
    RunMsi ('/i "' + $repairMsi + '" DESKTOPSHORTCUT=1 REINSTALL=ALL REINSTALLMODE=vomus') 'repair-same-package-new-path'
    Assert ((Get-FileHash $installed).Hash -eq (Get-FileHash (Join-Path $OutDir 'expected.exe')).Hash) 'Repair changed the binary.'
    RunMsi ('/x ' + $newCode) 'uninstall'
    Assert (!(Registered $newCode)) 'New product still installed.'
    Assert (!(Test-Path -LiteralPath $installed)) 'Uninstall left the Companion executable.'
    Assert ([IO.File]::ReadAllText($marker) -eq 'synthetic user data preserved') 'User data was changed.'
    Add-Content -LiteralPath $report -Value 'PASS: user data preserved; no Companion process launched.'
    exit 0
} catch {
    Add-Content -LiteralPath $report -Value $_.Exception.ToString()
    exit 1
}
