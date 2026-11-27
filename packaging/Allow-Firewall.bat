@echo off
rem Traffic-Test: allow incoming test traffic in Windows Defender Firewall.
rem Run once on each PC. Asks for administrator rights.
net session >nul 2>&1
if %errorlevel% neq 0 (
  powershell -NoProfile -Command "Start-Process -FilePath '%~f0' -Verb RunAs"
  exit /b
)
set EXE=%~dp0TrafficTest.exe
netsh advfirewall firewall delete rule name="Traffic-Test" >nul 2>&1
netsh advfirewall firewall add rule name="Traffic-Test" dir=in action=allow program="%EXE%" enable=yes profile=any >nul
netsh advfirewall firewall add rule name="Traffic-Test" dir=in action=allow protocol=TCP localport=5210 enable=yes profile=any >nul
netsh advfirewall firewall add rule name="Traffic-Test" dir=in action=allow protocol=UDP localport=5210-5338 enable=yes profile=any >nul
echo.
echo Traffic-Test firewall rules added (TCP 5210, UDP 5210-5338, TrafficTest.exe).
echo.
pause
