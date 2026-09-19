@echo off
setlocal

echo ==========================================
echo      GoImageBoardArchiver Build Script
echo ==========================================

REM Check Go installation
where go >nul 2>nul
if %ERRORLEVEL% NEQ 0 (
    echo Error: Go is not installed or not in PATH.
    exit /b 1
)

echo.
echo [1/3] Tidy up modules...
go mod tidy
if %ERRORLEVEL% NEQ 0 (
    echo Error: Failed to tidy modules.
    exit /b 1
)

echo.
echo [2/3] Building executable...
REM -s -w: Omit the symbol table and debug information to reduce binary size.
go build -ldflags="-s -w" -o giba.exe ./cmd/giba
if %ERRORLEVEL% NEQ 0 (
    echo Error: Build failed.
    exit /b 1
)

echo.
echo [3/3] Verifying resources...
if not exist "css\futaba.css" (
    echo Warning: 'css\futaba.css' not found.
    echo          Please ensure the 'css' directory exists and contains 'futaba.css'
    echo          before running the application.
) else (
    echo Resources found.
)

echo.
echo ==========================================
echo      Build Successful! -> giba.exe
echo ==========================================
endlocal
pause
