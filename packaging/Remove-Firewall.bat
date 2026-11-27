@echo off
rem Traffic-Test: remove the firewall rules added by Allow-Firewall.bat.
net session >nul 2>&1
if %errorlevel% neq 0 (
  powershell -NoProfile -Command "Start-Process -FilePath '%~f0' -Verb RunAs"
  exit /b
)
netsh advfirewall firewall delete rule name="Traffic-Test" >nul
echo.
echo Traffic-Test firewall rules removed.
echo.
pause
