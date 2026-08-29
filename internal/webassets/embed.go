package webassets

import (
	"embed"
	"io/fs"
)

// FS 是由 web/ 生成并嵌入二进制的生产前端资源。
// FS is the production frontend bundle generated from web/ and embedded in the binary.
//
//go:embed all:dist
var embedded embed.FS

var FS, _ = fs.Sub(embedded, "dist")

// BrandFS 包含未配置自定义资源时使用的内置 favicon 和 logo。
// BrandFS contains the fallback favicon and logo used when no custom asset is configured.
//
//go:embed brand/*
var BrandFS embed.FS
