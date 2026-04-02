@echo off
REM Build script for Brockchain TypeScript client

echo Building Brockchain TypeScript client...

call npm install --legacy-peer-deps
call npm run build

echo Build complete!
echo Output: lib\
dir /B lib\
