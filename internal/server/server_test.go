package server

import (
	"celler"
	"celler/java"
	"celler/layer_storage"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"neomega-blocks/ir"
	nbt "neomega-nbt/presets/v1"
	r2 "neomega-r2"
	"neomega-r2/blockactor"
	"neomega-r2/effector"
	"neomega-r2/object"
)

// 测试处理器证明 NBT 会到达扩展入口，而远景请求完全不调用它。
type testActor struct {
	calls int
	text  string
}

func (a *testActor) BlockNames() []string { return []string{"minecraft:stone"} }
func (a *testActor) Build(input blockactor.Input) ([]object.Descriptor, error) {
	a.calls++
	a.text = input.NBT["test_text"].(string)
	return []object.Descriptor{{Key: "test-actor", Kind: "test_panel", Position: [3]float64{1, 2, 3}, Replace: true}}, nil
}

func TestSessionAndNearObjectBoundary(t *testing.T) {
	path := t.TempDir()
	source, err := java.OpenDirectory(path, java.Options{})
	if err != nil {
		t.Fatal(err)
	}
	storage := layer_storage.MakePaletteStorage(ir.IRBlock("minecraft:air []"))
	storage.Set(layer_storage.At(1, 2, 3), "minecraft:stone []")
	storage.Set(layer_storage.At(2, 2, 3), "minecraft:enchanting_table []")
	chunk := java.NewChunk(celler.ChunkPos{}, 4189)
	chunk.NBT.Value["block_entities"] = []nbt.Compound{{"x": int32(1), "y": int32(2), "z": int32(3), "test_text": "牌面内容"}}
	if err = chunk.SetSection(&java.Section{Y: 0, Blocks: storage}); err != nil {
		t.Fatal(err)
	}
	if err = source.WriteChunk(java.Overworld, chunk); err != nil {
		t.Fatal(err)
	}
	if err = source.WriteLevel(nbt.Document{Value: nbt.Compound{"Data": nbt.Compound{"SpawnX": int32(7), "SpawnY": int32(30), "SpawnZ": int32(-9)}}}); err != nil {
		t.Fatal(err)
	}
	if err = source.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = source.Close(); err != nil {
		t.Fatal(err)
	}
	catalog, err := r2.OpenCatalog()
	if err != nil {
		t.Fatal(err)
	}
	actor := &testActor{}
	actors := blockactor.Registry{}
	actors.Register(actor)
	service := New(catalog, effector.Registry{"minecraft:enchanting_table": effector.Kind("enchanting_book")}, actors)
	defer service.Close()
	handler := service.Handler(fstest.MapFS{"index.html": {Data: []byte("空白欢迎页")}})
	request := func(method, url, body string) *httptest.ResponseRecorder {
		t.Helper()
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(method, url, strings.NewReader(body)))
		return response
	}
	if service.Info().Loaded {
		t.Fatal("启动时不应打开世界")
	}
	info, err := service.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Position != [3]float64{7, 42, -9} {
		t.Fatalf("出生点错误：%v", info.Position)
	}
	if _, err = service.Open(path + "/missing"); err == nil {
		t.Fatal("无效存档应失败")
	}
	if service.Info() != info {
		t.Fatal("打开失败改变了当前会话")
	}
	regionURL := fmt.Sprintf("/api/world/region?world=%d&x=0&z=0", info.ID)
	first := request("GET", regionURL, "")
	second := request("GET", regionURL, "")
	if first.Code != 200 || first.Body.String() != second.Body.String() {
		t.Fatal("区域缓存改变了数据")
	}
	if actor.calls != 0 {
		t.Fatal("远景调用了 NBT 外观处理器")
	}
	objectURL := fmt.Sprintf("/api/world/objects?world=%d&x=0&z=0&cx=0&cy=0&cz=0&radius=8", info.ID)
	response := request("GET", objectURL, "")
	var objects []object.Descriptor
	if err = json.Unmarshal(response.Body.Bytes(), &objects); err != nil {
		t.Fatal(err)
	}
	if len(objects) != 2 || actor.calls != 1 || actor.text != "牌面内容" {
		t.Fatalf("近景扩展失败：%+v %+v", objects, actor)
	}
	far := request("GET", strings.Replace(objectURL, "cx=0", "cx=100", 1), "")
	if far.Code != 200 || strings.TrimSpace(far.Body.String()) != "[]" || actor.calls != 1 {
		t.Fatal("近景范围外仍生成了特殊对象")
	}
	request("POST", "/api/world/close", "")
	if request("GET", regionURL, "").Code != 409 {
		t.Fatal("关闭后仍接受旧会话请求")
	}
	reopened, err := service.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.ID <= info.ID || request("GET", regionURL, "").Code != 409 {
		t.Fatal("重新打开后未拒绝旧会话")
	}
}
