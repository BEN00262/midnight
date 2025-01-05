@echo off
setlocal

:: Variables
set SERVICE_NAME=MidnightService2
set EXECUTABLE_PATH=C:\midnight\midnight.exe
set NSSM_URL=https://nssm.cc/release/nssm-2.24.zip
set NSSM_ZIP=%TEMP%\nssm.zip
set NSSM_DIR=%TEMP%\nssm

:: Ensure the script is run as administrator
net session >nul 2>&1
if %errorlevel% neq 0 (
    echo This script must be run as an administrator.
    pause
    exit /b
)

:: Download NSSM
echo Downloading NSSM...
powershell -Command "Invoke-WebRequest -Uri %NSSM_URL% -OutFile %NSSM_ZIP%"
if not exist %NSSM_ZIP% (
    echo Failed to download NSSM.
    pause
    exit /b
)

:: Extract NSSM
echo Extracting NSSM...
powershell -Command "Expand-Archive -Path %NSSM_ZIP% -DestinationPath %NSSM_DIR% -Force"
if not exist %NSSM_DIR% (
    echo Failed to extract NSSM.
    pause
    exit /b
)

:: Locate NSSM executable
set NSSM_EXE=%NSSM_DIR%\nssm-2.24\win64\nssm.exe
if not exist %NSSM_EXE% (
    echo NSSM executable not found.
    pause
    exit /b
)

:: Install the service using NSSM
echo Installing service %SERVICE_NAME%...
%NSSM_EXE% install %SERVICE_NAME% "%EXECUTABLE_PATH%"
if %errorlevel% neq 0 (
    echo Failed to install the service.
    pause
    exit /b
)

:: Start the service
echo Starting service %SERVICE_NAME%...
%NSSM_EXE% start %SERVICE_NAME%
if %errorlevel% neq 0 (
    echo Failed to start the service. Check NSSM logs for details.
    pause
    exit /b
)

:: Clean up
echo Cleaning up...
del /q %NSSM_ZIP%
rd /s /q %NSSM_DIR%

echo Service %SERVICE_NAME% installed and started successfully.