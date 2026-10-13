@echo off
rem Wrapper for reset-data.ps1 that works even when PowerShell script execution
rem is disabled. Arguments are passed through, e.g.
rem   scripts\reset-data.cmd -KeepReplays
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0reset-data.ps1" %*
