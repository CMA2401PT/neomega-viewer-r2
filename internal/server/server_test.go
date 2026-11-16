package server

import (
	"bytes"
	"celler"
	"celler/java"
	"celler/layer_storage"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"neomega-blocks/ir"
	nbt "neomega-nbt/presets/v1"
	r2 "neomega-r2"
	"neomega-r2/blockactor"
	"neomega-r2/effector"
	"neomega-r2/object"
	"neomega-viewer-r2/internal/worlddata"
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
		worlddata.StateMapping(catalog),
		effector.Registry{"minecraft:enchanting_table": effector.Kind("enchanting_book")},
		actors,
	)
	defer service.Close()
	handler := service.Handler(fstest.MapFS{"index.html": {Data: []byte("场景页面")}})
	request := func(method, url, body string) *httptest.ResponseRecorder {
		t.Helper()
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(method, url, strings.NewReader(body)))
		return response
	}
	if info := service.Info(); !info.Loaded || info.Scene == nil || info.Name != "Möbius" {
		t.Fatal("启动时应加载 Möbius", info)
	}
	info, err := service.Open(context.Background(), OpenOptions{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if info.Scene != nil {
		t.Fatal("存档不应携带内置场景设置")
	}
	if info.Position != [3]float64{8, 24, 8} {
		t.Fatalf("出生点错误：%v", info.Position)
	}
	if _, err = service.Open(context.Background(), OpenOptions{Path: path + "/missing"}); err == nil {
		t.Fatal("无效存档应失败")
	}
	if !reflect.DeepEqual(service.Info(), info) {
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
	if !reflect.DeepEqual(service.Info(), info) {
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
	reopened, err := service.Open(context.Background(), OpenOptions{Path: path})
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
	info, err := service.Open(context.Background(), OpenOptions{Path: path})
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
	reopened, err := service.Open(context.Background(), OpenOptions{Path: path})
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

// 内置场景沿用区域协议与地图占用协议，关闭后只更换会话编号，不改变场景内容。
func TestMobiusSession(t *testing.T) {
	catalog, err := r2.OpenCatalog()
	if err != nil {
		t.Fatal(err)
	}
	mapping := worlddata.StateMapping(catalog)
	service := New(mapping, nil, nil)
	defer service.Close()
	handler := service.Handler(fstest.MapFS{})
	request := func(url string) *httptest.ResponseRecorder {
		t.Helper()
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("GET", url, nil))
		if response.Code != 200 {
			t.Fatal(response.Code, response.Body.String())
		}
		return response
	}
	var info Info
	if err = json.Unmarshal(request("/api/world").Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if !info.Loaded || info.Name != "Möbius" || info.Scene == nil || info.Path != "" {
		t.Fatalf("默认场景信息错误：%+v", info)
	}
	if info.Scene.Ocean != 64 || info.Scene.Camera.Position != info.Position || info.Scene.Time != 19 {
		t.Fatalf("开场设置错误：%+v", info.Scene)
	}
	occupied := make(map[[2]int]bool)
	chunks := request(fmt.Sprintf("/api/world/chunks?world=%d", info.ID)).Body.Bytes()
	if len(chunks) == 0 || len(chunks)%136 != 0 {
		t.Fatal("场景占用表为空或记录不完整")
	}
	for offset := 0; offset < len(chunks); offset += 136 {
		rx := int(int32(binary.LittleEndian.Uint32(chunks[offset:])))
		rz := int(int32(binary.LittleEndian.Uint32(chunks[offset+4:])))
		for i := range 1024 {
			if chunks[offset+8+(i>>3)]&(1<<(i&7)) != 0 {
				occupied[[2]int{rx*32 + (i & 31), rz*32 + (i >> 5)}] = true
			}
		}
	}
	for _, position := range [][2]int{{-3, -1}, {-3, 0}, {2, -1}, {2, 0}} {
		if !occupied[position] {
			t.Fatalf("占用表缺少环所在区块 %v", position)
		}
	}
	lanterns := 0
	for rx := -1; rx <= 0; rx++ {
		for rz := -1; rz <= 0; rz++ {
			data := request(fmt.Sprintf(
				"/api/world/region?world=%d&x=%d&z=%d",
				info.ID,
				rx,
				rz,
			)).Body.Bytes()
			words := make([]uint32, len(data)/4)
			if err = binary.Read(bytes.NewReader(data), binary.LittleEndian, words); err != nil {
				t.Fatal(err)
			}
			if words[0] != 0x33504c54 || words[1] == 0 || words[2] != 0 {
				t.Fatalf("区域头错误：%v", words[:3])
			}
			cursor := 3
			for range words[1] {
				x, y, z := int(int32(words[cursor])), int(int32(words[cursor+1])), int(int32(words[cursor+2]))
				bits, count, length := words[cursor+3], int(words[cursor+4]), int(words[cursor+5])
				cursor += 6
				// 现有调色板存储直接读取传输索引，验证编码与 X-Z-Y 格子顺序兼容。
				storage := &layer_storage.Palette[uint32]{
					Level:         bits,
					IdxsPerUint32: 32 / bits,
					Mask:          (1 << bits) - 1,
					Palette:       words[cursor : cursor+count],
					Data:          words[cursor+count : cursor+count+length],
				}
				cursor += count + length
				if !occupied[[2]int{x, z}] {
					t.Fatalf("占用表未登记区域中的 section：%d %d %d", x, y, z)
				}
				for bx := range 16 {
					for bz := range 16 {
						for by := range 16 {
							model := storage.Get(layer_storage.At(bx, by, bz))
							if model == 0 {
								continue
							}
							if model != mapping["minecraft:sea_lantern []"] {
								t.Fatalf("出现海晶灯之外的模型：%d", model)
							}
							lanterns++
						}
					}
				}
			}
			if cursor != len(words) {
				t.Fatal("区域尾部有未解析数据")
			}
		}
	}
	if lanterns < 1000 {
		t.Fatalf("环没有完整生成：%d 格", lanterns)
	}
	objects := request(fmt.Sprintf(
		"/api/world/objects?world=%d&x=0&z=0&cx=0&cy=4&cz=0&radius=8",
		info.ID,
	))
	if strings.TrimSpace(objects.Body.String()) != "[]" {
		t.Fatal("内置场景应只有静态方块")
	}
	service.Close()
	closed := service.Info()
	if closed.ID <= info.ID || closed.Name != info.Name || *closed.Scene != *info.Scene {
		t.Fatalf("关闭后没有恢复默认场景：%+v", closed)
	}
}
