#ifndef MyVersion
#define MyVersion "0.0.0-dev"
#endif

#ifndef MyArch
#define MyArch "amd64"
#endif

#ifndef MyBinary
#define MyBinary "..\..\dist\fs-clip.exe"
#endif

#if MyArch == "amd64"
#define MyArchitecturesAllowed "x64compatible"
#define MyArchitecturesInstallIn64BitMode "x64compatible"
#elif MyArch == "arm64"
#define MyArchitecturesAllowed "arm64"
#define MyArchitecturesInstallIn64BitMode "arm64"
#else
#error Unsupported architecture
#endif

[Setup]
AppId={{6FA97F54-263B-4DBC-B433-42D7498804D4}
AppName=FSClip
AppVersion={#MyVersion}
AppPublisher=Nils Silbernagel
AppPublisherURL=https://github.com/N-Silbernagel/fs-clip
AppSupportURL=https://github.com/N-Silbernagel/fs-clip/issues
AppUpdatesURL=https://github.com/N-Silbernagel/fs-clip/releases
DefaultDirName={localappdata}\Programs\fs-clip
DefaultGroupName=FSClip
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
ArchitecturesAllowed={#MyArchitecturesAllowed}
ArchitecturesInstallIn64BitMode={#MyArchitecturesInstallIn64BitMode}
OutputDir=..\..\dist
OutputBaseFilename=fs-clip_{#MyVersion}_windows_{#MyArch}_setup
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
SetupLogging=yes
UninstallDisplayIcon={app}\fs-clip.exe
LicenseFile=..\..\LICENSE.txt

[Files]
Source: "{#MyBinary}"; DestDir: "{app}"; DestName: "fs-clip.exe"; Flags: ignoreversion

[Icons]
Name: "{group}\Open watched folder"; Filename: "{code:GetWatchDir}"
Name: "{group}\Uninstall FSClip"; Filename: "{uninstallexe}"

[Registry]
Root: HKCU; Subkey: "Software\FSClip"; ValueType: string; ValueName: "WatchDir"; ValueData: "{code:GetWatchDir}"

[Code]
var
  WatchDirPage: TInputDirWizardPage;
  ExistingInstallationDeactivated: Boolean;
  InstallationActivated: Boolean;

function GetWatchDir(Param: String): String;
begin
  Result := WatchDirPage.Values[0];
end;

procedure InitializeWizard;
var
  SavedWatchDir: String;
begin
  WatchDirPage := CreateInputDirPage(
    wpSelectDir,
    'Choose the watched folder',
    'Which folder should copy files to the clipboard?',
    'Files placed in this folder are copied to the clipboard and removed after a successful copy.',
    False,
    '');
  WatchDirPage.Add('');
  if RegQueryStringValue(HKCU, 'Software\FSClip', 'WatchDir', SavedWatchDir) then
    WatchDirPage.Values[0] := SavedWatchDir
  else
    WatchDirPage.Values[0] := ExpandConstant('{%USERPROFILE}\copy-to-clipboard');
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  ResultCode: Integer;
  Executable: String;
  InstallationRecord: String;
begin
  Result := '';
  if LoadStringFromFile(
    ExpandConstant('{localappdata}\fs-clip\installation.json'),
    InstallationRecord) and
    (Pos('"method": "script"', InstallationRecord) > 0) then
  begin
    Result := 'FSClip is currently managed by the command-line installer. Run "fs-clip uninstall" before using this installer.';
    Exit;
  end;
  Executable := ExpandConstant('{app}\fs-clip.exe');
  if FileExists(Executable) then
  begin
    if (not Exec(
      Executable,
      'deactivate --method inno',
      '',
      SW_HIDE,
      ewWaitUntilTerminated,
      ResultCode)) or (ResultCode <> 0) then
      Result := 'FSClip could not stop the existing installation. If it was installed from the command line, run "fs-clip uninstall" before using this installer.'
    else
      ExistingInstallationDeactivated := True;
  end;
end;

procedure CurStepChanged(CurStep: TSetupStep);
var
  ResultCode: Integer;
begin
  if CurStep = ssPostInstall then
  begin
    if (not Exec(
      ExpandConstant('{app}\fs-clip.exe'),
      'activate --method inno --watch-dir ' + AddQuotes(GetWatchDir('')),
      '',
      SW_HIDE,
      ewWaitUntilTerminated,
      ResultCode)) or (ResultCode <> 0) then
      RaiseException('FSClip was installed but could not configure automatic startup.')
    else
      InstallationActivated := True;
  end;
end;

procedure DeinitializeSetup;
var
  ResultCode: Integer;
  Executable: String;
begin
  if ExistingInstallationDeactivated and (not InstallationActivated) then
  begin
    Executable := ExpandConstant('{app}\fs-clip.exe');
    if FileExists(Executable) then
      Exec(
        Executable,
        'activate --method inno',
        '',
        SW_HIDE,
        ewWaitUntilTerminated,
        ResultCode);
  end;
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  ResultCode: Integer;
begin
  if CurUninstallStep = usUninstall then
  begin
    if (not Exec(
      ExpandConstant('{app}\fs-clip.exe'),
      'deactivate --method inno',
      '',
      SW_HIDE,
      ewWaitUntilTerminated,
      ResultCode)) or (ResultCode <> 0) then
      RaiseException('FSClip could not remove its automatic startup task.');
  end;
end;
