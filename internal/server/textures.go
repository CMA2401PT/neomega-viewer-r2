package server

import (
	"compress/gzip"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"

	"neomega-view-utils/r2assets"
)

// 浏览器明确禁用 gzip 时，即使同时接受通配编码也要返回原文。
func acceptsGzip(encoding string) bool {
	wildcard := false
	for _, value := range strings.Split(encoding, ",") {
		name, params, _ := strings.Cut(strings.TrimSpace(value), ";")
		allowed := true
		if key, q, ok := strings.Cut(strings.TrimSpace(params), "="); ok && strings.EqualFold(strings.TrimSpace(key), "q") {
			weight, err := strconv.ParseFloat(strings.TrimSpace(q), 64)
			allowed = err == nil && weight > 0 && weight <= 1
		}
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "gzip":
			return allowed
		case "*":
			wildcard = allowed
		}
	}
	return wildcard
}

// 默认资源复用编译时的压缩数据。按范围读取时使用原文件的字节位置。
func assetHandler() http.Handler {
	assets, _ := fs.Sub(r2assets.Files, "assets")
	plain := http.FileServerFS(assets)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Accept-Encoding")
		name := strings.TrimPrefix(r.URL.Path, "/")
		if (r.Method == http.MethodGet || r.Method == http.MethodHead) && r.Header.Get("Range") == "" && acceptsGzip(r.Header.Get("Accept-Encoding")) {
			body, length, err := r2assets.Files.OpenGzip("assets/" + name)
			if err == nil {
				contentType := mime.TypeByExtension(path.Ext(name))
				if contentType == "" {
					contentType = "application/octet-stream"
				}
				w.Header().Set("Content-Type", contentType)
				w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
				w.Header().Set("Content-Encoding", "gzip")
				if r.Method == http.MethodGet {
					io.Copy(w, body)
				}
				return
			}
		}
		plain.ServeHTTP(w, r)
	})
}

// 用户选择的材质和尺寸在请求时确定，因此边生成响应边压缩，避免另存整份传输缓冲区。
func writeTextureSet(w http.ResponseWriter, r *http.Request, textures *r2assets.TextureSet) {
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Add("Vary", "Accept-Encoding")
	if acceptsGzip(r.Header.Get("Accept-Encoding")) {
		w.Header().Set("Content-Encoding", "gzip")
		z, _ := gzip.NewWriterLevel(w, gzip.BestSpeed)
		defer z.Close()
		textures.WriteTo(z)
		return
	}
	textures.WriteTo(w)
}
