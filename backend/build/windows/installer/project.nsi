Unicode true

####
## Please note: Template replacements don't work in this file. They are provided with default defines like
## mentioned underneath.
## If the keyword is not defined, "wails_tools.nsh" will populate them with the values from ProjectInfo.
## If they are defined here, "wails_tools.nsh" will not touch them. This allows to use this project.nsi manually
## from outside of Wails for debugging and development of the installer.
##
## For development first make a wails nsis build to populate the "wails_tools.nsh":
## > wails build --target windows/amd64 --nsis
## Then you can call makensis on this file with specifying the path to your binary:
## For a AMD64 only installer:
## > makensis -DARG_WAILS_AMD64_BINARY=..\..\bin\app.exe
## For a ARM64 only installer:
## > makensis -DARG_WAILS_ARM64_BINARY=..\..\bin\app.exe
## For a installer with both architectures:
## > makensis -DARG_WAILS_AMD64_BINARY=..\..\bin\app-amd64.exe -DARG_WAILS_ARM64_BINARY=..\..\bin\app-arm64.exe
####
## Wails regenerates this file only when it is missing, so local edits here are
## preserved across builds. Keep it in sync after a Wails upgrade if the upstream
## template changes.
####

!include "wails_tools.nsh"

# The version information for this two must consist of 4 parts
VIProductVersion "${INFO_PRODUCTVERSION}.0"
VIFileVersion    "${INFO_PRODUCTVERSION}.0"

VIAddVersionKey "CompanyName"     "${INFO_COMPANYNAME}"
VIAddVersionKey "FileDescription" "${INFO_PRODUCTNAME} Installer"
VIAddVersionKey "ProductVersion"  "${INFO_PRODUCTVERSION}"
VIAddVersionKey "FileVersion"     "${INFO_PRODUCTVERSION}"
VIAddVersionKey "LegalCopyright"  "${INFO_COPYRIGHT}"
VIAddVersionKey "ProductName"     "${INFO_PRODUCTNAME}"

# Enable HiDPI support. https://nsis.sourceforge.io/Reference/ManifestDPIAware
ManifestDPIAware true

!include "MUI.nsh"

!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"
!define MUI_FINISHPAGE_NOAUTOCLOSE
!define MUI_ABORTWARNING

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "English"
!insertmacro MUI_LANGUAGE "SimpChinese"

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\bin\${INFO_PROJECTNAME}-${ARCH}-installer.exe"
InstallDir "$PROGRAMFILES64\${INFO_COMPANYNAME}\${INFO_PRODUCTNAME}"
ShowInstDetails show

Function .onInit
   !insertmacro wails.checkArchitecture
FunctionEnd

Section
    !insertmacro wails.setShellContext

    !insertmacro wails.webview2runtime

    SetOutPath $INSTDIR

    !insertmacro wails.files

    CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    CreateShortCut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"

    !insertmacro wails.associateFiles
    !insertmacro wails.associateCustomProtocols

    !insertmacro wails.writeUninstaller
SectionEnd

Section "uninstall"
    # wails.setShellContext sets $SHELL_CONTEXT to "all" for an admin-level
    # installer, which makes $APPDATA resolve to C:\ProgramData instead of the
    # user's roaming folder. The app data and the WebView2 cache both live under
    # the *user* profile, so select the current-user context for those paths.
    # $PROFILE always points at the invoking user, even when elevated.
    SetShellVarContext current

    # Wails caches the WebView2 profile in a folder named after the *executable*
    # it builds ("${PRODUCT_EXECUTABLE}"), which is not the folder the application
    # itself uses for its data. Remove the cache unconditionally.
    RMDir /r "$PROFILE\AppData\Roaming\${PRODUCT_EXECUTABLE}"

    # The application data (SQLite database, settings, logs) lives in
    # "$PROFILE\AppData\Roaming\GlobalHotNews" - ask before deleting it, because
    # it cannot be recovered. /SD IDNO makes the silent (/S) path keep the data.
    # NOTE: keep this string ASCII-only. NSIS reads this file without a BOM, so
    # non-ASCII here can be mis-decoded and break the script at compile time.
    MessageBox MB_YESNO|MB_ICONQUESTION "Also delete the application data?$\r$\n$\r$\nLocation: $PROFILE\AppData\Roaming\GlobalHotNews$\r$\nContains: collected news, data source settings, system settings and logs.$\r$\n$\r$\nYes = permanently delete this data (cannot be undone).$\r$\nNo  = keep the data so it is still available after reinstalling." /SD IDNO IDNO KeepUserData
    RMDir /r "$PROFILE\AppData\Roaming\GlobalHotNews"

KeepUserData:

    # Shortcuts and association keys follow the install-time context again.
    !insertmacro wails.setShellContext

    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"

    !insertmacro wails.unassociateFiles
    !insertmacro wails.unassociateCustomProtocols

    !insertmacro wails.deleteUninstaller

    # The uninstaller can still hold a handle on itself while it runs, so removing
    # the install folder may fail on the first pass; schedule it for reboot as well.
    RMDir /r $INSTDIR
    RMDir /REBOOTOK $INSTDIR
SectionEnd
