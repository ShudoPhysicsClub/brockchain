@echo off
REM Build script for Brockchain Go client (Windows only)

set OUTPUT_DIR=dist
if not exist %OUTPUT_DIR% mkdir %OUTPUT_DIR%

pushd "%~dp0"

echo Building Brockchain Go client...

REM Build for Windows
echo Building windows/amd64...
set GOOS=windows
set GOARCH=amd64
go build -o %OUTPUT_DIR%\brockchain-client-windows-amd64.exe .

echo Build complete!
dir /B %OUTPUT_DIR%

popd
