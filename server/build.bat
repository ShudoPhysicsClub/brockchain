@echo off
REM Build script for Brockchain server (Windows only)

set OUTPUT_DIR=dist
if not exist %OUTPUT_DIR% mkdir %OUTPUT_DIR%

echo Building Brockchain server...

REM Build for Windows
echo Building windows/amd64...
set GOOS=windows
set GOARCH=amd64
go build -o %OUTPUT_DIR%\brockchain-windows-amd64.exe .

echo Build complete!
dir /B %OUTPUT_DIR%
