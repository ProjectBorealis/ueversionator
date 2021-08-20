@echo off
setlocal
pushd %~dp0

rem Get the engine...
.\ueversionator.exe %*
if ERRORLEVEL 1 goto error

rem Done!
goto :EOF

rem Error happened. Wait for a keypress before quitting.
:error
pause
