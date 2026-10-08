# Tests only extract/read in the repository's .go-tmp directory.
# They never install/uninstall, start the companion, or access credentials.
[CmdletBinding()]
param(
    [string]$Exe = (Join-Path ([IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..'))) 'ForeverPulseCompanion.exe'),
    [string]$OutDir = ([IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..')))
)
$ErrorActionPreference='Stop'
Set-StrictMode -Version Latest
$repo=[IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..'))
$work=Join-Path $repo ('.go-tmp\installer-test-'+[guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $work | Out-Null
$Exe=[IO.Path]::GetFullPath($Exe)
$OutDir=[IO.Path]::GetFullPath($OutDir)
$info=[Diagnostics.FileVersionInfo]::GetVersionInfo($Exe)
$msiVersion='{0}.{1}.{2}' -f $info.FileMajorPart,$info.FileMinorPart,$info.FileBuildPart
# The in-service companion of this Windows account, if any, must stay untouched.
$live=Join-Path $env:LOCALAPPDATA 'Programs\ForeverPulseCompanion\ForeverPulseCompanion.exe'
$liveHash=if (Test-Path -LiteralPath $live) { (Get-FileHash -LiteralPath $live -Algorithm SHA256).Hash } else { $null }
$runKey=[Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Software\Microsoft\Windows\CurrentVersion\Run')
try { $startupBefore=if ($null -ne $runKey) { $runKey.GetValue('ForeverPulseCompanion') } else { $null } }
finally { if ($null -ne $runKey) { $runKey.Dispose() } }
$setup=Join-Path $OutDir 'ForeverPulseCompanion-Setup.exe'
function Assert([bool]$condition,[string]$message) { if (!$condition) { throw $message } }
function RunExe([string]$file,[string]$arguments) {
    try { $p=Start-Process -FilePath $file -ArgumentList $arguments -WindowStyle Hidden -Wait -PassThru }
    catch {
        if (!$_.Exception.Message.Contains('stratégie de contrôle')) { throw }
        # Retry the same file once; never alter/bypass the Windows policy.
        $p=Start-Process -FilePath $file -ArgumentList $arguments -WindowStyle Hidden -Wait -PassThru
    }
    Assert ($p.ExitCode -eq 0) ("Execution failed: $file (exit $($p.ExitCode))")
}
RunExe $setup ('--verify "'+$work+'"')
$verification=Get-Content -LiteralPath (Join-Path $work 'verification.txt') -Raw
Assert ($verification.Contains('MsiVerifyPackage: OK')) 'Invalid embedded MSI'
Assert ($verification.Contains('Installation executed: false')) 'Verification must never install'
Assert ($verification.Contains('Windows11 x64: True')) 'Tests expect Windows 11 x64'
$planning=Get-Content -LiteralPath (Join-Path $work 'planning.txt') -Raw
foreach ($action in @('FindRelatedProducts','AppSearch','LaunchConditions','CostInitialize','FileCost','CostFinalize','SetExpectedStartup','SetRegistryTool')) {
    Assert ($planning.Contains($action+': 0')) ('Native MSI planning failed: '+$action)
}
Assert ($planning.Contains('Startup ownership conditions: OK')) 'Native MSI startup guard failed'
Assert ($planning.Contains('Install transaction executed: false')) 'Planning must never install'
$msi=Join-Path $work 'companion.msi'
Assert ((Get-FileHash -LiteralPath $msi).Hash -eq (Get-FileHash -LiteralPath (Join-Path $OutDir 'ForeverPulseCompanion.msi')).Hash) 'Embedded MSI differs from generated MSI'
$com=New-Object -ComObject WindowsInstaller.Installer
$db=$com.OpenDatabase($msi,0)
function ComProperty($target,[string]$name,[object[]]$arguments=@()) {
    $raw=$target.PSObject.BaseObject
    $clean=New-Object object[] $arguments.Length
    for ($j=0;$j -lt $arguments.Length;$j++) { if ($null -ne $arguments[$j]) { $clean[$j]=$arguments[$j].PSObject.BaseObject } }
    $raw.GetType().InvokeMember($name,[Reflection.BindingFlags]::GetProperty,$null,$raw,$clean)
}
function Query([string]$sql) {
    $view=$db.OpenView($sql)
    try {
        $view.Execute() | Out-Null
        while ($null -ne ($record=$view.Fetch())) {
            try {
                $count=ComProperty $record 'FieldCount'
                $row=New-Object object[] $count
                for ($i=1;$i -le $count;$i++) { $row[$i-1]=ComProperty $record 'StringData' @($i) }
                ,$row
            } finally { [Runtime.InteropServices.Marshal]::FinalReleaseComObject($record) | Out-Null }
        }
    } finally { $view.Close() | Out-Null; [Runtime.InteropServices.Marshal]::FinalReleaseComObject($view) | Out-Null }
}
try {
    $props=@{}
    foreach ($row in (Query 'SELECT `Property`, `Value` FROM `Property`')) { $props[$row[0]]=$row[1] }
    Assert ($props.ProductVersion -eq $msiVersion) 'Wrong version'
    Assert (!$props.ContainsKey('ALLUSERS')) 'Package must default to per-user'
    Assert ($props.MSIRESTARTMANAGERCONTROL -eq 'Disable') 'No process may be terminated by restart manager'
    $signatureRows=@(Query 'SELECT `Signature` FROM `Signature`')
    Assert ($signatureRows.Count -eq 0) 'AppSearch requires an empty Signature table for registry-only searches'
    $fileRows=@(Query 'SELECT `File`, `Component_`, `FileName`, `Version` FROM `File`')
    Assert ($fileRows.Count -eq 3) 'Unexpected packaged files'
    Assert (@($fileRows | Where-Object { $_[0] -eq 'CompanionExe' -and $_[3] -eq "$msiVersion.0" }).Count -eq 1) 'Wrong executable version'
    $registryRows=@(Query 'SELECT `Root`, `Key`, `Name` FROM `Registry`')
    Assert ($registryRows.Count -eq 3) 'Unexpected registry writes'
    foreach ($row in $registryRows) {
        Assert ($row[0] -eq '1' -and $row[1] -eq 'Software\ForeverPulse\Companion\Installer') 'Only the installer HKCU anchor may be written'
    }
    $directories=@{}
    foreach ($row in (Query 'SELECT `Directory`, `Directory_Parent`, `DefaultDir` FROM `Directory`')) { $directories[$row[0]]=$row }
    Assert ($directories.INSTALLDIR[1] -eq 'ProgramsDir' -and $directories.ProgramsDir[1] -eq 'LocalAppDataFolder') 'Install location must be per-user'
    Assert (!$directories.ContainsKey('AppDataFolder')) 'Roaming application data must remain outside MSI ownership'
    foreach ($row in (Query 'SELECT `FileName`, `DirProperty`, `InstallMode` FROM `RemoveFile`')) {
        # Empty owned folders, plus the updater's own leftovers next to the program (never the program).
        $leftover=$row[0] -in @('FPC~1.OLD|ForeverPulseCompanion.exe.old','FPC~1.NEW|ForeverPulseCompanion.exe.new','FPC~1.REF|ForeverPulseCompanion.exe.refusee') -and $row[1] -eq 'INSTALLDIR'
        Assert (($leftover -or ([string]::IsNullOrEmpty($row[0]) -and $row[1] -in @('StartMenuDir','INSTALLDIR'))) -and $row[2] -eq '2') 'Uninstall may remove only empty owned folders and updater leftovers'
    }
    $shortcuts=@(Query 'SELECT `Directory_`, `Target` FROM `Shortcut`')
    Assert ($shortcuts.Count -eq 2) 'Start-menu and optional desktop shortcuts expected'
    foreach ($row in $shortcuts) { Assert ($row[1] -eq '[INSTALLDIR]ForeverPulseCompanion.exe') 'Unexpected shortcut target' }
    $actions=@(Query 'SELECT `Action`, `Type`, `Source`, `Target` FROM `CustomAction`')
    Assert ($actions.Count -eq 3) 'Unexpected custom actions'
    $cleanupAction=@($actions | Where-Object { $_[0] -eq 'CleanupOwnStartup' })
    Assert ($cleanupAction[0][1] -eq '50' -and $cleanupAction[0][2] -eq 'REGISTRYTOOL' -and $cleanupAction[0][3] -eq 'delete HKCU\Software\Microsoft\Windows\CurrentVersion\Run /v ForeverPulseCompanion /f') 'Unexpected startup cleanup action'
    Assert (@($actions | Where-Object { $_[0] -eq 'SetRegistryTool' -and $_[1] -eq '51' -and $_[3] -eq '[System64Folder]reg.exe' }).Count -eq 1) 'Use the Windows registry tool only'
    Assert (@($actions | Where-Object { $_[0] -eq 'SetExpectedStartup' -and $_[1] -eq '51' -and $_[3] -eq '"[INSTALLDIR]ForeverPulseCompanion.exe" --tray' }).Count -eq 1) 'Startup ownership reference missing'
    $cleanup=@(Query "SELECT ``Condition`` FROM ``InstallExecuteSequence`` WHERE ``Action``='CleanupOwnStartup'")
    Assert ($cleanup[0][0] -eq 'Installed AND REMOVE = "ALL" AND NOT UPGRADINGPRODUCTCODE AND EXISTINGSTARTUP ~= EXPECTEDSTARTUP') 'Startup cleanup must only run during uninstall and only for this install'
    $launch=@(Query 'SELECT `Condition` FROM `LaunchCondition`')
    Assert (@($launch | Where-Object { $_[0] -eq 'Installed OR (VersionNT64 AND WINDOWSBUILD >= 22000 AND NOT ALLUSERS)' }).Count -eq 1) 'Windows 11 x64 per-user guard missing'
    $upgrade=@(Query 'SELECT `UpgradeCode`, `ActionProperty` FROM `Upgrade`')
    Assert ($upgrade.Count -eq 2) 'Upgrade and downgrade detection expected'
    foreach ($row in $upgrade) { Assert ($row[0] -eq $props.UpgradeCode) 'Upgrade identity mismatch' }
} finally {
    [Runtime.InteropServices.Marshal]::FinalReleaseComObject($db) | Out-Null
    [Runtime.InteropServices.Marshal]::FinalReleaseComObject($com) | Out-Null
}
# Prove the native regression test rejects the exact defect from error 1603.
# This synthetic MSI copy is never installed and contains no personal data.
$broken=Join-Path $work 'missing-signature.msi'
Copy-Item -LiteralPath $msi -Destination $broken
$com=New-Object -ComObject WindowsInstaller.Installer
$db=$com.OpenDatabase($broken,1)
try {
    $view=$db.OpenView('DROP TABLE `Signature`')
    try { $view.Execute() | Out-Null } finally { $view.Close() | Out-Null; [Runtime.InteropServices.Marshal]::FinalReleaseComObject($view) | Out-Null }
    $db.Commit() | Out-Null
} finally {
    [Runtime.InteropServices.Marshal]::FinalReleaseComObject($db) | Out-Null
    [Runtime.InteropServices.Marshal]::FinalReleaseComObject($com) | Out-Null
}
$brokenReport=Join-Path $work 'broken-planning.txt'
$p=Start-Process -FilePath $setup -ArgumentList ('--check-msi "'+$broken+'" "'+$brokenReport+'"') -WindowStyle Hidden -Wait -PassThru
Assert ($p.ExitCode -eq 1) 'A package without Signature must fail native planning'
Assert ((Get-Content -LiteralPath $brokenReport -Raw).Contains('AppSearch: 1603')) 'Missing Signature regression was not detected at AppSearch'
$cab=Join-Path $work 'companion.cab'
$extracted=Join-Path $work 'files'
New-Item -ItemType Directory $extracted | Out-Null
& "$env:WINDIR\System32\expand.exe" $cab '-F:*' $extracted > (Join-Path $work 'expand.log')
Assert ($LASTEXITCODE -eq 0) 'Cabinet extraction failed'
foreach ($pair in @(@('CompanionExe',$Exe),@('GuideFR',(Join-Path $repo 'LISEZMOI.md')),@('GuideEN',(Join-Path $repo 'README.md')))) {
    Assert ((Get-FileHash -LiteralPath (Join-Path $extracted $pair[0])).Hash -eq (Get-FileHash -LiteralPath $pair[1]).Hash) ('Packaged content mismatch: '+$pair[0])
}
RunExe $setup ('--preview "'+(Join-Path $work 'preview.png')+'"')
Assert ((Get-Item -LiteralPath (Join-Path $work 'preview.png')).Length -gt 5000) 'Preview render is empty'
$runKey=[Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Software\Microsoft\Windows\CurrentVersion\Run')
try { $startupAfter=if ($null -ne $runKey) { $runKey.GetValue('ForeverPulseCompanion') } else { $null } }
finally { if ($null -ne $runKey) { $runKey.Dispose() } }
Assert ($startupAfter -eq $startupBefore) 'Existing startup entry was changed'
if ($null -ne $liveHash) { Assert ((Get-FileHash -LiteralPath $live).Hash -eq $liveHash) 'In-service executable was changed' }
Write-Output 'PASS: native MSI search/conditions/costing; missing-Signature regression rejected; startup ownership conditions; package validation; embedded MSI; per-user paths; exact payload and guides; shortcut targets; uninstall boundaries; version guards; upgrade identity; preview; existing live executable/startup unchanged.'
Write-Output "Test files: $work"
Write-Output 'No installation, uninstallation or companion launch executed.'
