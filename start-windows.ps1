# Windows D3D11 驱动编译大型射线程序可能极慢；独立浏览器配置使用 OpenGL。
param([int]$Port = 2412)
$ErrorActionPreference = 'Stop'
$executable = Join-Path $PSScriptRoot 'dist/neomega-viewer-r2.exe'
if (-not (Test-Path -LiteralPath $executable)) {
    throw "Build dist/neomega-viewer-r2.exe first."
}
$candidates = @(
    "${env:ProgramFiles}/Google/Chrome/Application/chrome.exe",
    "${env:ProgramFiles(x86)}/Microsoft/Edge/Application/msedge.exe",
    "$env:LOCALAPPDATA/Google/Chrome/Application/chrome.exe"
)
$browser = $candidates | Where-Object { Test-Path -LiteralPath $_ } | Select-Object -First 1
if (-not $browser) { throw 'Chrome or Edge is required.' }
$url = "http://127.0.0.1:$Port"
# 只复用已就绪的 R2 服务；其他程序占用端口时报告错误，不擅自关闭。
function Test-R2Server {
    try {
        $response = Invoke-WebRequest "$url/r2/render/renderer.js" -UseBasicParsing -TimeoutSec 2
        return $response.StatusCode -eq 200 -and $response.Content.Contains('createRenderer')
    } catch { return $false }
}
if (-not (Test-R2Server)) {
    $server = Start-Process -FilePath $executable -ArgumentList '-listen', "127.0.0.1:$Port", '-browser=false' -WindowStyle Hidden -PassThru
    $ready = $false
    for ($attempt = 0; $attempt -lt 60; $attempt++) {
        if ($server.HasExited) { throw "R2 exited. Check whether port $Port is occupied." }
        if (Test-R2Server) { $ready = $true; break }
        Start-Sleep -Milliseconds 500
    }
    if (-not $ready) { throw "R2 did not become ready at $url." }
}
# 必须独立配置目录，否则已有 Chrome/Edge 进程会忽略新的 GPU 后端参数。
$profile = Join-Path $env:LOCALAPPDATA 'neomega-viewer-r2/browser-opengl'
Start-Process -FilePath $browser -ArgumentList '--use-angle=gl', "--user-data-dir=`"$profile`"", '--no-first-run', "--app=$url"
