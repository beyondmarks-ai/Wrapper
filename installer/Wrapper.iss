#define MyAppName "Wrapper"
#define MyAppVersion "1.7.0"
#define MyAppPublisher "Beyond Marks"
#define MyAppExeName "wrap.exe"

[Setup]
AppId={{D0A27051-7D9B-4AC3-B429-9DBA137701D1}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
DefaultDirName={autopf}\Wrapper
DefaultGroupName=Wrapper
PrivilegesRequired=admin
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
OutputDir=..\dist
OutputBaseFilename=Wrapper-Setup-x64
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern
UninstallDisplayIcon={app}\wrap.exe
ChangesEnvironment=yes
LicenseFile=..\LICENSE

[Tasks]
Name: "addtopath"; Description: "Add Wrapper to my PATH"; Flags: checkedonce

[Files]
Source: "..\bin\wrap.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\bin\wrapper-agent.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\bin\Everything64.dll"; DestDir: "{app}"; Flags: ignoreversion
Source: "prerequisites\Everything-1.4.1.1032.x64.msi"; DestDir: "{tmp}"; Flags: deleteafterinstall
Source: "..\README.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\LICENSE"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\NOTICE.md"; DestDir: "{app}"; Flags: ignoreversion

[Registry]
Root: HKLM; Subkey: "SYSTEM\CurrentControlSet\Control\Session Manager\Environment"; ValueType: expandsz; ValueName: "Path"; ValueData: "{olddata};{app}"; Tasks: addtopath; Check: NeedsAddPath(ExpandConstant('{app}'))

[Icons]
Name: "{group}\Wrapper"; Filename: "{app}\wrap.exe"
Name: "{group}\Wrapper Cloud Setup"; Filename: "powershell.exe"; Parameters: "-NoExit -Command ""& ''{app}\wrap.exe'' auth login; if ($LASTEXITCODE -eq 0) {{ & ''{app}\wrap.exe'' device list *> $null; if ($LASTEXITCODE -ne 0) {{ & ''{app}\wrap.exe'' device register --name $env:COMPUTERNAME } else {{ & ''{app}\wrap.exe'' agent start } }"""

[Run]
Filename: "{sys}\msiexec.exe"; Parameters: "/i ""{tmp}\Everything-1.4.1.1032.x64.msi"" /qn /norestart EVERYTHING_SERVICE=1 START_ON_STARTUP=1 DESKTOP_SHORTCUT=0 START_MENU_SHORTCUT=0 EFU_ASSOCIATION=0"; StatusMsg: "Installing Everything search and indexing..."; Flags: waituntilterminated; Check: NeedsEverythingInstall
Filename: "{autopf}\Everything\Everything.exe"; Parameters: "-startup"; StatusMsg: "Starting Everything indexer..."; Flags: runasoriginaluser nowait; Check: FileExists(ExpandConstant('{autopf}\Everything\Everything.exe'))
Filename: "powershell.exe"; Parameters: "-NoExit -Command ""& '{app}\wrap.exe' auth login; if ($LASTEXITCODE -eq 0) {{ & '{app}\wrap.exe' device list *> $null; if ($LASTEXITCODE -ne 0) {{ & '{app}\wrap.exe' device register --name $env:COMPUTERNAME } else {{ & '{app}\wrap.exe' agent start } }"""; Description: "Sign in and register this PC with Wrapper"; Flags: postinstall nowait runasoriginaluser skipifsilent

[UninstallRun]
Filename: "{app}\wrap.exe"; Parameters: "agent uninstall"; Flags: runhidden; RunOnceId: "RemoveWrapperAgent"

[Code]
function NeedsAddPath(Param: string): Boolean;
var
  CurrentPath: string;
begin
  if not RegQueryStringValue(HKLM, 'SYSTEM\CurrentControlSet\Control\Session Manager\Environment', 'Path', CurrentPath) then
    CurrentPath := '';
  Result := Pos(';' + Uppercase(Param) + ';', ';' + Uppercase(CurrentPath) + ';') = 0;
end;

function NeedsEverythingInstall: Boolean;
begin
  Result := not (RegKeyExists(HKLM, 'SYSTEM\CurrentControlSet\Services\Everything') and
    FileExists(ExpandConstant('{autopf}\Everything\Everything.exe')));
end;
