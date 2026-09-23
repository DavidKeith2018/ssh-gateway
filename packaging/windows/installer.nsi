Unicode True
!include "MUI2.nsh"
!include "x64.nsh"
!ifndef VERSION
!define VERSION "0.1.0"
!endif
!ifndef PROJECT_ROOT
!define PROJECT_ROOT "..\.."
!endif
Name "SSH Gateway"
OutFile "${PROJECT_ROOT}\dist\ssh-gateway-desktop-${VERSION}-windows-x64-setup.exe"
InstallDir "$LOCALAPPDATA\Programs\SSH Gateway"
RequestExecutionLevel user
!define MUI_ABORTWARNING
!define MUI_ICON "${PROJECT_ROOT}\packaging\icon.ico"
!define MUI_UNICON "${PROJECT_ROOT}\packaging\icon.ico"
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"
Function .onInit
 ${IfNot} ${RunningX64}
  MessageBox MB_ICONSTOP "This package requires 64-bit Windows."
  Abort
 ${EndIf}
FunctionEnd
Function .onInstSuccess
 ; Microsoft documents these 32-bit registry entries for Evergreen Runtime detection.
 SetRegView 32
 ReadRegStr $0 HKLM "SOFTWARE\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" "pv"
 ${If} $0 == ""
 ${OrIf} $0 == "0.0.0.0"
  ReadRegStr $0 HKCU "SOFTWARE\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" "pv"
 ${EndIf}
 ${If} $0 == ""
 ${OrIf} $0 == "0.0.0.0"
  MessageBox MB_OK|MB_ICONINFORMATION "SSH Gateway is installed. Microsoft WebView2 was not detected. Before opening the app, run MicrosoftEdgeWebview2Setup.exe in the installation folder to install the runtime. Internet access is required." /SD IDOK
 ${EndIf}
FunctionEnd
Section
 SetOutPath "$INSTDIR"
 File "${PROJECT_ROOT}\dist\ssh-gateway-desktop-windows.exe"
 File "${PROJECT_ROOT}\packaging\windows\MicrosoftEdgeWebview2Setup.exe"
 ; Keep the signed runtime bootstrapper available for manual installation only.
 WriteUninstaller "$INSTDIR\uninstall.exe"
 CreateShortcut "$DESKTOP\SSH Gateway.lnk" "$INSTDIR\ssh-gateway-desktop-windows.exe"
 CreateShortcut "$SMPROGRAMS\SSH Gateway.lnk" "$INSTDIR\ssh-gateway-desktop-windows.exe"
 WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\SSH Gateway" "DisplayName" "SSH Gateway"
 WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\SSH Gateway" "DisplayVersion" "${VERSION}"
 WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\SSH Gateway" "UninstallString" '"$INSTDIR\uninstall.exe"'
SectionEnd
Section "Uninstall"
 IfFileExists "$INSTDIR\ssh-gateway-desktop-windows.exe" 0 remove_shortcuts
 ClearErrors
 Delete "$INSTDIR\ssh-gateway-desktop-windows.exe"
 IfErrors 0 remove_shortcuts
 MessageBox MB_ICONSTOP "Close SSH Gateway before uninstalling."
 Abort
 remove_shortcuts:
 Delete "$DESKTOP\SSH Gateway.lnk"
 Delete "$SMPROGRAMS\SSH Gateway.lnk"
 Delete "$INSTDIR\ssh-gateway-desktop-windows.exe"
 Delete "$INSTDIR\MicrosoftEdgeWebview2Setup.exe"
 Delete "$INSTDIR\uninstall.exe"
 RMDir "$INSTDIR"
 DeleteRegKey HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\SSH Gateway"
 ; Keep the separate user data directory during uninstall.
SectionEnd
