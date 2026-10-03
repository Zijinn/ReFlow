Unicode true

!include "MUI2.nsh"
!include "FileFunc.nsh"

!ifndef VERSION
  !define VERSION "2.0.1"
!endif
!ifndef APP_EXE
  !error "APP_EXE must point to ReFlow.exe"
!endif
!ifndef APP_ICON
  !error "APP_ICON must point to ReFlow.ico"
!endif
!ifndef LICENSE_FILE
  !error "LICENSE_FILE must point to LICENSE"
!endif
!ifndef WEBVIEW2_BOOTSTRAPPER
  !error "WEBVIEW2_BOOTSTRAPPER must point to MicrosoftEdgeWebview2Setup.exe"
!endif
!ifndef WEB_ASSETS
  !error "WEB_ASSETS must point to the built web/dist directory"
!endif
!ifndef OUT_FILE
  !define OUT_FILE "ReFlow-${VERSION}-windows-x64-setup.exe"
!endif

Name "ReFlow"
OutFile "${OUT_FILE}"
InstallDir "$LOCALAPPDATA\Programs\ReFlow"
InstallDirRegKey HKCU "Software\ReFlow" "InstallDir"
RequestExecutionLevel user
ManifestDPIAware true

VIProductVersion "${VERSION}.0"
VIFileVersion "${VERSION}.0"
VIAddVersionKey "CompanyName" "ReFlow contributors"
VIAddVersionKey "FileDescription" "ReFlow installer"
VIAddVersionKey "ProductVersion" "${VERSION}"
VIAddVersionKey "FileVersion" "${VERSION}"
VIAddVersionKey "LegalCopyright" "Copyright 2026 ReFlow contributors. GPL-3.0-only."
VIAddVersionKey "ProductName" "ReFlow"

!define MUI_ABORTWARNING
!define MUI_ICON "${APP_ICON}"
!define MUI_UNICON "${APP_ICON}"

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_LICENSE "${LICENSE_FILE}"
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

Section "ReFlow" SEC_REFLOW
  SetShellVarContext current
  SetOutPath "$INSTDIR"
  File /oname=ReFlow.exe "${APP_EXE}"
  File /oname=LICENSE.txt "${LICENSE_FILE}"
  SetOutPath "$INSTDIR\web\dist"
  File /r "${WEB_ASSETS}\*"

  SetOutPath "$PLUGINSDIR"
  File /oname=MicrosoftEdgeWebview2Setup.exe "${WEBVIEW2_BOOTSTRAPPER}"
  ExecWait '"$PLUGINSDIR\MicrosoftEdgeWebview2Setup.exe" /silent /install'

  SetOutPath "$INSTDIR"
  WriteUninstaller "$INSTDIR\Uninstall.exe"
  WriteRegStr HKCU "Software\ReFlow" "InstallDir" "$INSTDIR"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\ReFlow" "DisplayName" "ReFlow"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\ReFlow" "DisplayVersion" "${VERSION}"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\ReFlow" "Publisher" "ReFlow contributors"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\ReFlow" "DisplayIcon" "$INSTDIR\ReFlow.exe"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\ReFlow" "UninstallString" '"$INSTDIR\Uninstall.exe"'
  ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
  WriteRegDWORD HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\ReFlow" "EstimatedSize" $0
  CreateDirectory "$SMPROGRAMS\ReFlow"
  CreateShortcut "$SMPROGRAMS\ReFlow\ReFlow.lnk" "$INSTDIR\ReFlow.exe"
  CreateShortcut "$DESKTOP\ReFlow.lnk" "$INSTDIR\ReFlow.exe"
SectionEnd

Section "Uninstall"
  SetShellVarContext current
  Delete "$DESKTOP\ReFlow.lnk"
  Delete "$SMPROGRAMS\ReFlow\ReFlow.lnk"
  RMDir "$SMPROGRAMS\ReFlow"
  Delete "$INSTDIR\ReFlow.exe"
  Delete "$INSTDIR\LICENSE.txt"
  Delete "$INSTDIR\Uninstall.exe"
  RMDir "$INSTDIR"
  DeleteRegKey HKCU "Software\ReFlow"
  DeleteRegKey HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\ReFlow"
SectionEnd
