@echo off

cd /d "%~dp0"

echo Opening http://127.0.0.1:8080/

echo Keep this window open while you use the app. Press Ctrl+C to stop.

start "" "http://127.0.0.1:8080/"

job-review-mcp.exe

pause

