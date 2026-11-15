// Package viewerr2 提供应用页面；渲染资源由 neomega-r2 单独提供。
package viewerr2

import "embed"

//go:embed web/*
var Web embed.FS
