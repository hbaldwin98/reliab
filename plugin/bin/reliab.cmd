@echo off
rem reliab launcher for shells that cannot run the sh launcher (PowerShell,
rem cmd). Delegates to it through Git Bash.
setlocal
set "RELIAB_SH=sh"
where sh >nul 2>nul || set "RELIAB_SH=%ProgramFiles%\Git\bin\sh.exe"
"%RELIAB_SH%" "%~dp0reliab" %*
exit /b %ERRORLEVEL%
