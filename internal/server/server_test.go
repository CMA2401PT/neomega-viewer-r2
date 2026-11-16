package server

import (
	"bytes"
	"celler"
	"celler/java"
	"celler/layer_storage"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"neomega-blocks/ir"
	nbt "neomega-nbt/presets/v1"
	r2 "neomega-r2"
	"neomega-r2/blockactor"
	"neomega-r2/effector"
	"neomega-r2/object"
	"neomega-viewer-r2/internal/javaworld"
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
	service := New(
		javaworld.StateMapping(catalog),
		effector.Registry{"minecraft:enchanting_table": effector.Kind("enchanting_book")},
		actors,
	)
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
	textures := request("POST", "/api/textures", `{"path":"","maxSize":32}`)
	if textures.Code != 200 {
		t.Fatal(textures.Code, textures.Body.String())
	}
	if service.Info() != info {
		t.Fatal("材质包接口改变了存档会话")
	}
	wire := textures.Body.Bytes()
	headerLength := binary.LittleEndian.Uint32(wire)
	var textureHeader struct {
		Info struct {
			Side   int
			Levels int
			Layers int
		}
		Files []struct {
			Name   string
			Length int
		}
	}
	if err = json.Unmarshal(wire[4:4+headerLength], &textureHeader); err != nil {
		t.Fatal(err)
	}
	if textureHeader.Info.Side != 16 || textureHeader.Info.Levels != 5 || textureHeader.Info.Layers != 563 || len(textureHeader.Files) != 11 {
		t.Fatal(textureHeader)
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

// 稀疏存档覆盖正负区域坐标与位图边界，空区域和下界记录不应混入主世界占用表。
func TestWorldChunks(t *testing.T) {
	path := t.TempDir()
	source, err := java.OpenDirectory(path, java.Options{})
	if err != nil {
		t.Fatal(err)
	}
	positions := []celler.ChunkPos{
		{X: 0, Z: 0}, {X: 7, Z: 0}, {X: 8, Z: 0}, {X: 31, Z: 31},
		{X: -1, Z: -1}, {X: -32, Z: -32}, {X: -33, Z: 32}, {X: 32, Z: -1},
	}
	for _, pos := range positions {
		if err = source.WriteChunk(java.Overworld, java.NewChunk(pos, 4189)); err != nil {
			t.Fatal(err)
		}
	}
	empty := celler.ChunkPos{X: 64, Z: 64}
	if err = source.WriteChunk(java.Overworld, java.NewChunk(empty, 4189)); err != nil {
		t.Fatal(err)
	}
	if err = source.Delete(java.Overworld, java.Terrain, empty); err != nil {
		t.Fatal(err)
	}
	if err = source.WriteChunk(java.Nether, java.NewChunk(empty, 4189)); err != nil {
		t.Fatal(err)
	}
	if err = source.WriteLevel(nbt.Document{Value: nbt.Compound{"Data": nbt.Compound{}}}); err != nil {
		t.Fatal(err)
	}
	if err = source.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = source.Close(); err != nil {
		t.Fatal(err)
	}
	service := New(nil, nil, nil)
	defer service.Close()
	info, err := service.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	handler := service.Handler(fstest.MapFS{})
	request := func(id uint64) *httptest.ResponseRecorder {
		t.Helper()
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("GET", fmt.Sprintf("/api/world/chunks?world=%d", id), nil))
		return response
	}
	first := request(info.ID)
	if first.Code != 200 || first.Header().Get("Content-Type") != "application/octet-stream" {
		t.Fatal(first.Code, first.Header(), first.Body.String())
	}
	data := first.Body.Bytes()
	if len(data) != 4*136 {
		t.Fatalf("期望 4 条非空区域记录，得到 %d 字节", len(data))
	}
	want := map[celler.ChunkPos][128]byte{
		{X: 0, Z: 0}:   {0: 0x81, 1: 0x01, 127: 0x80},
		{X: -1, Z: -1}: {0: 0x01, 127: 0x80},
		{X: -2, Z: 1}:  {3: 0x80},
		{X: 1, Z: -1}:  {124: 0x01},
	}
	for offset := 0; offset < len(data); offset += 136 {
		pos := celler.ChunkPos{
			X: int32(binary.LittleEndian.Uint32(data[offset:])),
			Z: int32(binary.LittleEndian.Uint32(data[offset+4:])),
		}
		bitmap, ok := want[pos]
		if !ok || !bytes.Equal(data[offset+8:offset+136], bitmap[:]) {
			t.Fatalf("区域 %v 的位图错误：%x", pos, data[offset+8:offset+136])
		}
		delete(want, pos)
	}
	// 删除文件后仍应命中会话缓存；重新打开则扫描为空，并拒绝旧会话。
	files, err := filepath.Glob(filepath.Join(path, "region", "*.mca"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if err = os.Remove(file); err != nil {
			t.Fatal(err)
		}
	}
	if cached := request(info.ID); cached.Code != 200 || !bytes.Equal(cached.Body.Bytes(), data) {
		t.Fatal("占用表未命中会话缓存")
	}
	reopened, err := service.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if request(info.ID).Code != 409 {
		t.Fatal("切换后仍接受旧会话的占用表请求")
	}
	if empty := request(reopened.ID); empty.Code != 200 || empty.Body.Len() != 0 {
		t.Fatal("空存档的占用表应为零字节")
	}
	service.Close()
	if request(reopened.ID).Code != 409 {
		t.Fatal("关闭后仍接受占用表请求")
	}
}
