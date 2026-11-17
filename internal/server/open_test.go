package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	nbt "neomega-nbt/presets/v1"
)

// 流式打开需把进度和最终会话放在同一响应；失败时保留可用画面。
func TestStreamingStructureOpen(t *testing.T) {
	raw, err := nbt.EncodeToNBTBytes(nbt.Compound{
		"size":    []int32{1, 1, 1},
		"palette": []nbt.Compound{{"Name": "minecraft:stone"}},
		"blocks":  []nbt.Compound{{"pos": []int32{0, 0, 0}, "state": int32(0)}},
	}, nbt.OptEncodeToNBT{Encoding: nbt.EncodingBigEndian})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "one.nbt")
	if err := os.WriteFile(path, raw, 0644); err != nil {
		t.Fatal(err)
	}
	server := New(nil, nil)
	defer server.Close()
	handler := server.Handler(fstest.MapFS{})
	for _, memory := range []int{32, 64} {
		before := server.Info().ID
		body, err := json.Marshal(map[string]any{"path": path, "structureMemoryMiB": memory, "structureWorkers": 1, "bedrockFix": false})
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest("POST", "/api/world/open", bytes.NewReader(body))
		request.Header.Set("Accept", "application/x-ndjson")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		decoder := json.NewDecoder(response.Body)
		progress := 0
		terminal := ""
		for {
			var message struct {
				Type  string
				World Info
			}
			err := decoder.Decode(&message)
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if message.Type == "progress" {
				progress++
				continue
			}
			terminal = message.Type
			if terminal == "world" && (message.World.StructureMemoryMiB != 64 || message.World.StructureWorkers != 1 || message.World.BedrockFix || message.World.MinY != -64 || message.World.Height != 512) {
				t.Fatal(message.World)
			}
		}
		if memory == 32 {
			if terminal != "error" || server.Info().ID != before {
				t.Fatal(terminal, server.Info())
			}
		} else if terminal != "world" || progress == 0 || server.Info().ID == before {
			t.Fatal(terminal, progress, server.Info())
		}
	}
}
