@echo off
REM ============================================================================
REM Mumble 兼容性代理 - Windows 启动脚本
REM ============================================================================

setlocal EnableDelayedExpansion

REM 默认配置
set MURMUR_HOST=%1
if "%MURMUR_HOST%"=="" set MURMUR_HOST=localhost

set MURMUR_TCP=%2
if "%MURMUR_TCP%"=="" set MURMUR_TCP=64738

set MURMUR_UDP=%3
if "%MURMUR_UDP%"=="" set MURMUR_UDP=64738

REM 代理端口
set WS_PORT=8080
set TCP_PROXY_PORT=64739
set UDP_PROXY_PORT=64739
set METRICS_PORT=9090

echo.
echo ============================================================
echo        Mumble Compatibility Proxy - Local Bridge
echo ============================================================
echo.

REM 检查 Go 是否安装
where go >nul 2>nul
if %ERRORLEVEL% neq 0 (
    echo 错误: Go 未安装，请先安装 Go
    echo 下载地址: https://go.dev/dl/
    exit /b 1
)

REM 编译代理
echo 编译代理...
go build -o mumble-proxy.exe .

REM 设置环境变量
set WS_PORT=%WS_PORT%
set TCP_PORT=%TCP_PROXY_PORT%
set UDP_PORT=%UDP_PROXY_PORT%
set METRICS_PORT=%METRICS_PORT%
set MURMUR_HOST=%MURMUR_HOST%
set MURMUR_TCP=%MURMUR_TCP%
set MURMUR_UDP=%MURMUR_UDP%

echo.
echo 启动配置:
echo   WebSocket 端口:  %WS_PORT% (Web 客户端连接)
echo   TCP 代理端口:    %TCP_PROXY_PORT% (旧客户端连接)
echo   UDP 代理端口:    %UDP_PROXY_PORT% (音频)
echo   指标端口:        %METRICS_PORT% (Prometheus)
echo.
echo Murmur 后端:
echo   主机: %MURMUR_HOST%
echo   TCP:  %MURMUR_TCP%
echo   UDP:  %MURMUR_UDP%
echo.

echo 启动代理...
echo.
echo 连接方式:
echo   Web 客户端:  ws://localhost:%WS_PORT%/ws
echo   旧客户端:    连接到 localhost:%TCP_PROXY_PORT%
echo   指标:        http://localhost:%METRICS_PORT%/metrics
echo   健康检查:    http://localhost:%WS_PORT%/health
echo.
echo 按 Ctrl+C 停止代理
echo.

mumble-proxy.exe