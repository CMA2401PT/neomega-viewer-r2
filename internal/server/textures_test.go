package server

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"neomega-view-utils/r2assets"
)

// 校验浏览器自动解压后的字节，同时覆盖显式禁用 gzip 和按范围读取原文件。
func TestTextureEncoding(t *testing.T) {
	assets := assetHandler()
	name := "fast.bin"
	original, err := r2assets.Files.ReadFile("assets/" + name)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		encoding, byteRange string
		wantGzip            bool
	}{
		{"br, gzip", "", true},
		{"gzip;q=0, *;q=1", "", false},
		{"gzip", "bytes=0-15", false},
	} {
		req := httptest.NewRequest(http.MethodGet, "/"+name, nil)
		req.Header.Set("Accept-Encoding", tc.encoding)
		req.Header.Set("Range", tc.byteRange)
		resp := httptest.NewRecorder()
		assets.ServeHTTP(resp, req)
		if (resp.Header().Get("Content-Encoding") == "gzip") != tc.wantGzip {
			t.Fatalf("错误的响应编码：%v", resp.Header())
		}
		got, want := resp.Body.Bytes(), original
		if tc.wantGzip {
			r, err := gzip.NewReader(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			got, err = io.ReadAll(r)
			r.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
		if tc.byteRange != "" {
			if resp.Code != http.StatusPartialContent {
				t.Fatal(resp.Code)
			}
			want = original[:16]
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("资源传输改变了 %s 的内容", name)
		}
	}
}
