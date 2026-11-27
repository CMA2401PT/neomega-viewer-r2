// Package viewerr2 提供随 Viewer 发布的宿主页面和已经编译好的 R2 前端模块。
package viewerr2

import "embed"

//go:embed web/*.html web/*.css web/dist/*.js web/dist/r2/*.js web/dist/r2/*.css
var Web embed.FS
