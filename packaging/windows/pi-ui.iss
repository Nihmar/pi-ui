; Inno Setup script: the Windows installer for the pi-ui server.
;
; Build it on Windows (or on CI) after `flutter build windows --release` and
; `go build ./cmd/pi-ui`:
;
;   iscc /DAppVersion=0.1.0 /DSourceRoot=..\..\dist packaging\windows\pi-ui.iss
;
; The build is unsigned: the plan's Phase 8 says signing comes later, and a self-signed
; certificate would train users to click through a warning rather than verify anything.
#define AppName "pi-ui"
#define AppPublisher "Nihmar"
#define AppUrl "https://github.com/Nihmar/pi-ui"
#ifndef AppVersion
  #define AppVersion "0.0.1"
#endif
#ifndef SourceRoot
  #define SourceRoot "..\..\dist"
#endif

[Setup]
AppId={{6C1F0B2E-8A4D-4C6E-9B7A-2F5D3C1E7A90}
AppName={#AppName}
AppVersion={#AppVersion}
AppPublisher={#AppPublisher}
AppPublisherURL={#AppUrl}
AppSupportURL={#AppUrl}/issues
DefaultDirName={autopf}\{#AppName}
DefaultGroupName={#AppName}
DisableProgramGroupPage=yes
OutputBaseFilename=pi-ui-{#AppVersion}-setup
Compression=lzma2
SolidCompression=yes
ArchitecturesInstallIn64BitMode=x64compatible
UninstallDisplayIcon={app}\pi-ui.exe
WizardStyle=modern
; The server is a service-like process, not a desktop app: no startup shortcut.
PrivilegesRequired=lowest
PrivilegesRequiredOverridesAllowed=dialog

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Files]
Source: "{#SourceRoot}\pi-ui.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#SourceRoot}\pi-ui-bridge.ts"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#SourceRoot}\LICENSE"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#SourceRoot}\README.md"; DestDir: "{app}"; Flags: ignoreversion isreadme

[Icons]
Name: "{group}\pi-ui server"; Filename: "{app}\pi-ui.exe"; Parameters: "serve --root ""%USERPROFILE%\Projects"""; Comment: "Start the server"
Name: "{group}\pi-ui status"; Filename: "{app}\pi-ui.exe"; Parameters: "status"
Name: "{group}\Uninstall pi-ui"; Filename: "{uninstallexe}"

[Run]
Filename: "{app}\pi-ui.exe"; Parameters: "status"; Description: "Show the paired devices"; Flags: postinstall shellexec skipifsilent
