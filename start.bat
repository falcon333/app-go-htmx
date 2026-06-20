@echo off
REM ============================================================
REM  Strategy Portfolio Builder - one-click launcher
REM  Starts portfolio-server.exe and opens the browser.
REM ============================================================

cd /d "%~dp0"

REM Build the exe if it doesn't exist yet
if not exist "portfolio-server.exe" (
    echo Building portfolio-server.exe ...
    go build -o portfolio-server.exe ./cmd/server
    if errorlevel 1 (
        echo.
        echo Build failed. Make sure Go is installed and you are in app-go-htmx.
        pause
        exit /b 1
    )
)

REM Start the server in its own window
start "Portfolio Server" portfolio-server.exe

REM Give it a moment to bind to port 8080, then open the browser
timeout /t 2 /nobreak >nul
start "" "http://localhost:8080/import/batch"

echo.
echo Server launched at http://localhost:8080/import/batch
echo Close the "Portfolio Server" window to stop it.
