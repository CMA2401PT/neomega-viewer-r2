package server

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

// 校验浏览器自动解压后的字节，同时覆盖显式禁用 gzip 和按范围读取原文件。
func TestTextureEncoding(t *testing.T) {
	bundle := testBrowser(t)
	assets := assetHandler(bundle.Materials)
	name := "fast.bin"
	original, err := bundle.Materials.ReadFile("assets/" + name)
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

// 载入自选材质时，浏览器应能通过原 URL 请求到本应用配套的合成信息。
func TestMaterialRecipeURL(t *testing.T) {
	service := New(testWorlds(t))
	defer service.Close()
	handler := service.Handler(fstest.MapFS{}, testBrowser(t))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/r2/material-info.zip", nil))
	want := testBrowser(t).Recipe
	if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), want) {
		t.Fatal("未返回应用提供的材质合成信息包", response.Code)
	}
}
