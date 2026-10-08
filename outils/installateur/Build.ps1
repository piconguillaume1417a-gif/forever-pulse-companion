# Build an offline, per-user Windows 11 x64 package using Windows tools only.
# 0.10.0: any version. The version is read from the executable's version resource
# (never by running it): display string "0.10.0-rc.3", Windows number 0.10.3
# (internal/update.NumeroWindows, written by outils/mkres).
[CmdletBinding()]
param(
    [string]$Exe = (Join-Path ([IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..'))) 'ForeverPulseCompanion.exe'),
    [string]$OutDir = ([IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..')))
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$repo = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..'))
$work = Join-Path $repo '.go-tmp\installateur'
$payload = [IO.Path]::GetFullPath($Exe)
New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
$OutDir = [IO.Path]::GetFullPath($OutDir)
$msiPath = Join-Path $OutDir 'ForeverPulseCompanion.msi'
$setupPath = Join-Path $OutDir 'ForeverPulseCompanion-Setup.exe'
$csc = Join-Path $env:WINDIR 'Microsoft.NET\Framework64\v4.0.30319\csc.exe'
if (!(Test-Path -LiteralPath $csc)) { throw 'Compilateur .NET Framework x64 absent.' }
$info = [Diagnostics.FileVersionInfo]::GetVersionInfo($payload)
$display = $info.ProductVersion
$msiVersion = '{0}.{1}.{2}' -f $info.FileMajorPart, $info.FileMinorPart, $info.FileBuildPart
if ($display -notmatch '^\d+\.\d+\.\d+(-rc\.\d+)?$' -or $info.FileBuildPart -eq 0) {
    throw "Version du compagnon inattendue ($display, $msiVersion) : reconstruire la ressource avec outils/mkres."
}
# One ProductCode per version, derived from it: the same version always gives the same code.
$md5 = [Security.Cryptography.MD5]::Create().ComputeHash([Text.Encoding]::UTF8.GetBytes("ForeverPulseCompanion/ProductCode/$msiVersion"))
$productCode = '{' + ([guid]::new($md5)).ToString().ToUpperInvariant() + '}'
if (Test-Path -LiteralPath $work) { Remove-Item -LiteralPath $work -Recurse -Force }
New-Item -ItemType Directory -Force -Path $work | Out-Null
$files = @(
    @{ Id='CompanionExe'; Path=$payload; Name='ForeverPulseCompanion.exe'; Component='Companion'; Version="$msiVersion.0" },
    @{ Id='GuideFR'; Path=(Join-Path $repo 'LISEZMOI.md'); Name='LISEZMOI.md'; Component='Guides'; Version=$null },
    @{ Id='GuideEN'; Path=(Join-Path $repo 'README.md'); Name='README.md'; Component='Guides'; Version=$null }
)
$ddf = @('.OPTION EXPLICIT', '.Set CabinetNameTemplate=companion.cab', ".Set DiskDirectoryTemplate=$work", '.Set CompressionType=MSZIP', '.Set Cabinet=on', '.Set Compress=on', '.Set MaxDiskSize=0')
foreach ($file in $files) { $ddf += ('"{0}" {1}' -f $file.Path, $file.Id) }
$ddfPath = Join-Path $work 'package.ddf'
[IO.File]::WriteAllLines($ddfPath, $ddf, [Text.Encoding]::Default)
Push-Location $work
try {
    & "$env:WINDIR\System32\makecab.exe" /F $ddfPath > (Join-Path $work 'makecab.log')
    if ($LASTEXITCODE -ne 0) { throw "makecab: $LASTEXITCODE" }
} finally { Pop-Location }
if (Test-Path -LiteralPath $msiPath) { Remove-Item -LiteralPath $msiPath }
$installer = New-Object -ComObject WindowsInstaller.Installer
function Invoke-Com($obj, [string]$method, [object[]]$arguments, [Reflection.BindingFlags]$flags=[Reflection.BindingFlags]::InvokeMethod) {
    $clean = New-Object object[] $arguments.Length
    for ($j=0; $j -lt $arguments.Length; $j++) {
        if ($null -ne $arguments[$j]) { $clean[$j] = $arguments[$j].PSObject.BaseObject }
    }
    $target = $obj.PSObject.BaseObject
    $target.GetType().InvokeMember($method, $flags, $null, $target, $clean)
}
function Set-Com($obj, [string]$property, [object[]]$arguments) {
    $clean = New-Object object[] $arguments.Length
    for ($j=0; $j -lt $arguments.Length; $j++) {
        if ($null -ne $arguments[$j]) { $clean[$j] = $arguments[$j].PSObject.BaseObject }
    }
    $target = $obj.PSObject.BaseObject
    $target.GetType().InvokeMember($property, [Reflection.BindingFlags]::SetProperty, $null, $target, $clean) | Out-Null
}
$db = Invoke-Com $installer 'OpenDatabase' @($msiPath, 3)
$columnsByTable = @{}
function Sql([string]$query, $record=$null) {
    $view = Invoke-Com $db 'OpenView' @($query)
    try {
        if ($null -eq $record) { Invoke-Com $view 'Execute' @() | Out-Null }
        else { Invoke-Com $view 'Execute' @($record) | Out-Null }
    } catch { throw "MSI SQL failed: $query : $_" }
    finally { Invoke-Com $view 'Close' @() | Out-Null; [Runtime.InteropServices.Marshal]::FinalReleaseComObject($view) | Out-Null }
}
function Table([string]$name, [string]$columns, [string]$primary) {
    Sql ('CREATE TABLE `{0}` ({1} PRIMARY KEY {2})' -f $name,$columns,$primary)
    $columnsByTable[$name] = ([regex]::Matches($columns, '`[^`]+`') | ForEach-Object { $_.Value }) -join ','
}
function Row([string]$table, [object[]]$values) {
    $r = Invoke-Com $installer 'CreateRecord' @($values.Length)
    try {
        for ($i=0; $i -lt $values.Length; $i++) {
            if ($null -eq $values[$i]) { continue }
            if ($values[$i] -is [int]) { Set-Com $r 'IntegerData' @(($i+1),$values[$i]) }
            else { Set-Com $r 'StringData' @(($i+1),[string]$values[$i]) }
        }
        $marks = (@('?') * $values.Length) -join ','
        Sql ('INSERT INTO `{0}` ({1}) VALUES ({2})' -f $table,$columnsByTable[$table],$marks) $r
    } finally { [Runtime.InteropServices.Marshal]::FinalReleaseComObject($r) | Out-Null }
}
try {
    Table 'Property' '`Property` CHAR(72) NOT NULL, `Value` CHAR(0) NOT NULL' '`Property`'
    Table 'Directory' '`Directory` CHAR(72) NOT NULL, `Directory_Parent` CHAR(72), `DefaultDir` CHAR(255) NOT NULL' '`Directory`'
    Table 'Component' '`Component` CHAR(72) NOT NULL, `ComponentId` CHAR(38), `Directory_` CHAR(72) NOT NULL, `Attributes` SHORT NOT NULL, `Condition` CHAR(255), `KeyPath` CHAR(72)' '`Component`'
    Table 'Feature' '`Feature` CHAR(38) NOT NULL, `Feature_Parent` CHAR(38), `Title` CHAR(64), `Description` CHAR(255), `Display` SHORT, `Level` SHORT NOT NULL, `Directory_` CHAR(72), `Attributes` SHORT NOT NULL' '`Feature`'
    Table 'FeatureComponents' '`Feature_` CHAR(38) NOT NULL, `Component_` CHAR(72) NOT NULL' '`Feature_`, `Component_`'
    Table 'File' '`File` CHAR(72) NOT NULL, `Component_` CHAR(72) NOT NULL, `FileName` CHAR(255) NOT NULL, `FileSize` LONG NOT NULL, `Version` CHAR(72), `Language` CHAR(20), `Attributes` SHORT, `Sequence` LONG NOT NULL' '`File`'
    Table 'Media' '`DiskId` SHORT NOT NULL, `LastSequence` LONG NOT NULL, `DiskPrompt` CHAR(64), `Cabinet` CHAR(255), `VolumeLabel` CHAR(32), `Source` CHAR(72)' '`DiskId`'
    Table 'Registry' '`Registry` CHAR(72) NOT NULL, `Root` SHORT NOT NULL, `Key` CHAR(255) NOT NULL, `Name` CHAR(255), `Value` CHAR(0), `Component_` CHAR(72) NOT NULL' '`Registry`'
    Table 'Shortcut' '`Shortcut` CHAR(72) NOT NULL, `Directory_` CHAR(72) NOT NULL, `Name` CHAR(128) NOT NULL, `Component_` CHAR(72) NOT NULL, `Target` CHAR(255) NOT NULL, `Arguments` CHAR(255), `Description` CHAR(255), `Hotkey` SHORT, `Icon_` CHAR(72), `IconIndex` SHORT, `ShowCmd` SHORT, `WkDir` CHAR(72)' '`Shortcut`'
    Table 'RemoveFile' '`FileKey` CHAR(72) NOT NULL, `Component_` CHAR(72) NOT NULL, `FileName` CHAR(255), `DirProperty` CHAR(72) NOT NULL, `InstallMode` SHORT NOT NULL' '`FileKey`'
    Table 'Upgrade' '`UpgradeCode` CHAR(38) NOT NULL, `VersionMin` CHAR(20), `VersionMax` CHAR(20), `Language` CHAR(255), `Attributes` LONG NOT NULL, `Remove` CHAR(255), `ActionProperty` CHAR(72) NOT NULL' '`UpgradeCode`, `VersionMin`, `VersionMax`, `Language`, `Attributes`'
    Table 'LaunchCondition' '`Condition` CHAR(255) NOT NULL, `Description` CHAR(255) NOT NULL' '`Condition`'
    Table 'AppSearch' '`Property` CHAR(72) NOT NULL, `Signature_` CHAR(72) NOT NULL' '`Property`, `Signature_`'
    # AppSearch requires this table even when all searches are registry values.
    # Leave it empty: adding signatures would change these into file searches.
    Table 'Signature' '`Signature` CHAR(72) NOT NULL, `FileName` CHAR(255) NOT NULL, `MinVersion` CHAR(20), `MaxVersion` CHAR(20), `MinSize` LONG, `MaxSize` LONG, `MinDate` LONG, `MaxDate` LONG, `Languages` CHAR(255)' '`Signature`'
    Table 'RegLocator' '`Signature_` CHAR(72) NOT NULL, `Root` SHORT NOT NULL, `Key` CHAR(255) NOT NULL, `Name` CHAR(255), `Type` SHORT' '`Signature_`'
    Table 'InstallExecuteSequence' '`Action` CHAR(72) NOT NULL, `Condition` CHAR(255), `Sequence` SHORT' '`Action`'
    Table 'InstallUISequence' '`Action` CHAR(72) NOT NULL, `Condition` CHAR(255), `Sequence` SHORT' '`Action`'
    Table 'AdminExecuteSequence' '`Action` CHAR(72) NOT NULL, `Condition` CHAR(255), `Sequence` SHORT' '`Action`'
    Table 'AdminUISequence' '`Action` CHAR(72) NOT NULL, `Condition` CHAR(255), `Sequence` SHORT' '`Action`'
    Table 'CustomAction' '`Action` CHAR(72) NOT NULL, `Type` SHORT NOT NULL, `Source` CHAR(72), `Target` CHAR(255)' '`Action`'
    $properties = [ordered]@{
        ProductCode=$productCode
        UpgradeCode='{FCFDDE35-D056-434B-B281-E99553B41900}'
        ProductName='Forever Pulse Companion'; ProductVersion=$msiVersion; ProductLanguage='1033'
        Manufacturer='Forever Pulse'; INSTALLLEVEL='1'; ARPNOMODIFY='1'; ARPNOREPAIR='1'
        ARPCOMMENTS="Forever Pulse Companion $display. Settings and data are kept on uninstall."
        ARPURLINFOABOUT='https://forever-pulse.com'; MSIRESTARTMANAGERCONTROL='Disable'
        SecureCustomProperties='OLDVERSION;NEWERVERSION'
    }
    foreach ($p in $properties.GetEnumerator()) { Row 'Property' @($p.Key,$p.Value) }
    Row 'Directory' @('TARGETDIR',$null,'SourceDir')
    Row 'Directory' @('LocalAppDataFolder','TARGETDIR','.')
    Row 'Directory' @('ProgramsDir','LocalAppDataFolder','Programs')
    Row 'Directory' @('INSTALLDIR','ProgramsDir','FPComp~1|ForeverPulseCompanion')
    Row 'Directory' @('ProgramMenuFolder','TARGETDIR','.')
    Row 'Directory' @('StartMenuDir','ProgramMenuFolder','FPulse|Forever Pulse')
    Row 'Directory' @('DesktopFolder','TARGETDIR','.')
    Row 'Directory' @('System64Folder','TARGETDIR','.')
    # Component GUIDs never change: every version installs the same files at the same place.
    Row 'Component' @('Companion','{27D20F86-C4D3-4289-9166-0D552AAE9201}','INSTALLDIR',260,$null,'InstalledCompanion')
    Row 'Component' @('Guides','{C07519E6-5FFD-42FA-8166-2632D99B4402}','INSTALLDIR',260,$null,'InstalledGuides')
    Row 'Component' @('DesktopLink','{A6482F5A-1203-45D7-9F71-3F547878D403}','INSTALLDIR',324,'DESKTOPSHORTCUT = "1"','InstalledDesktopLink')
    Row 'Feature' @('Main',$null,'Forever Pulse Companion','Companion and guides',1,1,'INSTALLDIR',0)
    foreach ($component in @('Companion','Guides','DesktopLink')) { Row 'FeatureComponents' @('Main',$component) }
    $sequence=0
    foreach ($file in $files) {
        $sequence++
        $short = @('FPC.exe','LISEZMOI.md','README.md','FPMaint.exe')[$sequence-1]
        Row 'File' @($file.Id,$file.Component,($short+'|'+$file.Name),[int](Get-Item -LiteralPath $file.Path).Length,$file.Version,$null,512,$sequence)
    }
    Row 'Media' @(1,$sequence,$null,'#companion.cab',$null,$null)
    foreach ($component in @('Companion','Guides','DesktopLink')) {
        Row 'Registry' @(('Installed'+$component),1,'Software\ForeverPulse\Companion\Installer',$component,$display,$component)
    }
    Row 'Shortcut' @('StartLink','StartMenuDir','FPComp|Forever Pulse Companion','Companion','[INSTALLDIR]ForeverPulseCompanion.exe',$null,'Forever Pulse Companion',$null,$null,$null,1,'INSTALLDIR')
    Row 'Shortcut' @('DesktopShortcut','DesktopFolder','FPComp|Forever Pulse Companion','DesktopLink','[INSTALLDIR]ForeverPulseCompanion.exe',$null,'Forever Pulse Companion',$null,$null,$null,1,'INSTALLDIR')
    # Empty folders only, never wildcard deletes and never the application data folder.
    Row 'RemoveFile' @('RemoveStartFolder','Companion',$null,'StartMenuDir',2)
    Row 'RemoveFile' @('RemoveInstallFolder','Companion',$null,'INSTALLDIR',2)
    # Leftovers of the automatic updater (internal/update), never the program itself.
    Row 'RemoveFile' @('RemoveUpdateOld','Companion','FPC~1.OLD|ForeverPulseCompanion.exe.old','INSTALLDIR',2)
    Row 'RemoveFile' @('RemoveUpdateNew','Companion','FPC~1.NEW|ForeverPulseCompanion.exe.new','INSTALLDIR',2)
    Row 'RemoveFile' @('RemoveUpdateRefused','Companion','FPC~1.REF|ForeverPulseCompanion.exe.refusee','INSTALLDIR',2)
    Row 'Upgrade' @($properties.UpgradeCode,'0.0.0',$msiVersion,$null,257,$null,'OLDVERSION')
    Row 'Upgrade' @($properties.UpgradeCode,$msiVersion,$null,$null,2,$null,'NEWERVERSION')
    Row 'RegLocator' @('WindowsBuild',2,'SOFTWARE\Microsoft\Windows NT\CurrentVersion','CurrentBuildNumber',18)
    Row 'AppSearch' @('WINDOWSBUILD','WindowsBuild')
    Row 'RegLocator' @('ExistingStartup',1,'SOFTWARE\Microsoft\Windows\CurrentVersion\Run','ForeverPulseCompanion',18)
    Row 'AppSearch' @('EXISTINGSTARTUP','ExistingStartup')
    Row 'LaunchCondition' @('Installed OR (VersionNT64 AND WINDOWSBUILD >= 22000 AND NOT ALLUSERS)','Windows 11 x64 required, per-user installation / Windows 11 x64 requis, installation pour votre compte.')
    Row 'LaunchCondition' @('Installed OR NOT NEWERVERSION','A newer Forever Pulse Companion is already installed / Une version plus recente est deja installee.')
    $actions = [ordered]@{
        FindRelatedProducts=25; AppSearch=50; LaunchConditions=100; CostInitialize=800
        FileCost=900; CostFinalize=1000; MigrateFeatureStates=1200; InstallValidate=1400
        InstallInitialize=1500; RemoveExistingProducts=1501; ProcessComponents=1600
        UnpublishFeatures=1800; RemoveShortcuts=3200; RemoveFiles=3500
        RemoveRegistryValues=2600; InstallFiles=4000; CreateShortcuts=4500
        WriteRegistryValues=5000; RegisterUser=6000; RegisterProduct=6100
        PublishFeatures=6300; PublishProduct=6400; InstallFinalize=6600
    }
    foreach ($a in $actions.GetEnumerator()) { Row 'InstallExecuteSequence' @($a.Key,$null,[int]$a.Value) }
    # Native signed Windows tool, current-user context, exact ownership guard.
    # No script execution, credentials access or companion process termination.
    Row 'CustomAction' @('SetExpectedStartup',51,'EXPECTEDSTARTUP','"[INSTALLDIR]ForeverPulseCompanion.exe" --tray')
    Row 'CustomAction' @('SetRegistryTool',51,'REGISTRYTOOL','[System64Folder]reg.exe')
    Row 'CustomAction' @('CleanupOwnStartup',50,'REGISTRYTOOL','delete HKCU\Software\Microsoft\Windows\CurrentVersion\Run /v ForeverPulseCompanion /f')
    Row 'InstallExecuteSequence' @('SetExpectedStartup',$null,1050)
    Row 'InstallExecuteSequence' @('SetRegistryTool',$null,1051)
    Row 'InstallExecuteSequence' @('CleanupOwnStartup','Installed AND REMOVE = "ALL" AND NOT UPGRADINGPRODUCTCODE AND EXISTINGSTARTUP ~= EXPECTEDSTARTUP',2500)
    foreach ($a in @('FindRelatedProducts','AppSearch','LaunchConditions','CostInitialize','FileCost','CostFinalize','MigrateFeatureStates')) {
        Row 'InstallUISequence' @($a,$null,[int]$actions[$a])
    }
    Row 'InstallUISequence' @('ExecuteAction',$null,1300)
    foreach ($a in @('CostInitialize','FileCost','CostFinalize','InstallValidate','InstallInitialize','InstallFiles','InstallFinalize')) {
        Row 'AdminExecuteSequence' @($a,$null,[int]$actions[$a])
    }
    Row 'AdminExecuteSequence' @('InstallAdminPackage',$null,3900)
    foreach ($a in @('CostInitialize','FileCost','CostFinalize')) {
        Row 'AdminUISequence' @($a,$null,[int]$actions[$a])
    }
    Row 'AdminUISequence' @('ExecuteAction',$null,1300)
    $stream = Invoke-Com $installer 'CreateRecord' @(2)
    try {
        Set-Com $stream 'StringData' @(1,'companion.cab')
        Invoke-Com $stream 'SetStream' @(2,(Join-Path $work 'companion.cab')) | Out-Null
        Sql 'INSERT INTO `_Streams` (`Name`, `Data`) VALUES (?, ?)' $stream
    } finally { [Runtime.InteropServices.Marshal]::FinalReleaseComObject($stream) | Out-Null }
    Invoke-Com $db 'Commit' @() | Out-Null
} finally { [Runtime.InteropServices.Marshal]::FinalReleaseComObject($db) | Out-Null }
# Summary metadata is persisted after closing and reopening the database.
$summary = Invoke-Com $installer 'SummaryInformation' @($msiPath,20) ([Reflection.BindingFlags]::GetProperty)
try {
    foreach ($p in @(@(1,1252),@(2,'Installation Forever Pulse Companion'),@(3,"Forever Pulse Companion $display"),@(4,'Forever Pulse'),@(7,'x64;1033'),@(9,('{'+[guid]::NewGuid().ToString().ToUpperInvariant()+'}')),@(14,200),@(15,10),@(18,'Forever Pulse build script'),@(19,2))) {
        Set-Com $summary 'Property' $p
    }
    Invoke-Com $summary 'Persist' @() | Out-Null
} finally {
    [Runtime.InteropServices.Marshal]::FinalReleaseComObject($summary) | Out-Null
    [Runtime.InteropServices.Marshal]::FinalReleaseComObject($installer) | Out-Null
}
$msiHash = (Get-FileHash -LiteralPath $msiPath -Algorithm SHA256).Hash
$companionHash = (Get-FileHash -LiteralPath $payload -Algorithm SHA256).Hash
$metadata = @"
[assembly: System.Reflection.AssemblyVersion("$msiVersion.0")]
[assembly: System.Reflection.AssemblyFileVersion("$msiVersion.0")]
[assembly: System.Reflection.AssemblyInformationalVersion("$display")]
internal static class PackageInfo {
    internal const string Version = "$display";
    internal const string MsiHash = "$msiHash";
    internal const string CompanionHash = "$companionHash";
    internal const string ProductCode = "$productCode";
}
"@
$metadataPath = Join-Path $work 'PackageInfo.cs'
[IO.File]::WriteAllText($metadataPath,$metadata,[Text.Encoding]::UTF8)
& $csc /nologo /target:winexe /platform:x64 /optimize+ /warnaserror+ /utf8output "/out:$setupPath" "/win32manifest:$PSScriptRoot\setup.manifest" "/win32icon:$PSScriptRoot\setup.ico" /reference:System.Windows.Forms.dll /reference:System.Drawing.dll "/resource:$msiPath,companion.msi" "/resource:$repo\internal\icone\forever.png,logo.png" "$PSScriptRoot\Setup.cs" "$PSScriptRoot\InspectMsi.cs" $metadataPath
if ($LASTEXITCODE -ne 0) { throw "csc: $LASTEXITCODE" }
Write-Output "Version: $display (MSI $msiVersion, ProductCode $productCode)"
Write-Output "Created: $setupPath"
Write-Output "MSI SHA256: $msiHash"
Write-Output "Companion SHA256: $companionHash"
