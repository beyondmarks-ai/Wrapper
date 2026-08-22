#define MyAppName "Wrapper"
#define MyAppVersion "1.6.0"
#define MyAppPublisher "Beyond Marks"
#define MyAppExeName "wrap.exe"

[Setup]
AppId={{D0A27051-7D9B-4AC3-B429-9DBA137701D1}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
DefaultDirName={localappdata}\Programs\Wrapper
DefaultGroupName=Wrapper
PrivilegesRequired=lowest
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
Source: "..\README.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\LICENSE"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\NOTICE.md"; DestDir: "{app}"; Flags: ignoreversion

[Registry]
Root: HKA; Subkey: "Environment"; ValueType: expandsz; ValueName: "Path"; ValueData: "{olddata};{app}"; Tasks: addtopath; Check: NeedsAddPath(ExpandConstant('{app}'))

[Icons]
Name: "{group}\Wrapper"; Filename: "{app}\wrap.exe"
Name: "{group}\Wrapper Cloud Setup"; Filename: "powershell.exe"; Parameters: "-NoExit -Command ""wrap auth login; wrap device register --name $env:COMPUTERNAME"""

[Run]
Filename: "{app}\wrap.exe"; Parameters: "--help"; Description: "Open Wrapper command help"; Flags: postinstall nowait skipifsilent

[UninstallRun]
Filename: "{app}\wrap.exe"; Parameters: "agent uninstall"; Flags: runhidden; RunOnceId: "RemoveWrapperAgent"

[Code]
function NeedsAddPath(Param: string): Boolean;
var
  CurrentPath: string;
begin
  if not RegQueryStringValue(HKA, 'Environment', 'Path', CurrentPath) then
    CurrentPath := '';
  Result := Pos(';' + Uppercase(Param) + ';', ';' + Uppercase(CurrentPath) + ';') = 0;
end;
