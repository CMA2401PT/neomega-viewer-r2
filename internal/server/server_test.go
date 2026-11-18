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
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/coder/websocket"
	"neomega-blocks/ir"
	nbt "neomega-nbt/presets/v1"
	"neomega-r2link"
	"neomega-r2link/blockactor"
	"neomega-r2link/effector"
	"neomega-r2link/object"
)

// 测试处理器证明 NBT 会到达扩展入口，而远景区域完全不调用它。
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
	actor := &testActor{}
	actors := blockactor.Registry{}
	actors.Register(actor)
	service := New(
		effector.Registry{"minecraft:enchanting_table": effector.Kind("enchanting_book")},
		actors,
	)
	defer service.Close()
	handler := service.Handler(fstest.MapFS{"index.html": {Data: []byte("场景页面")}})
	server := httptest.NewServer(handler)
	defer server.Close()
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
	client, welcome, err := dialWorld(t, server, info.ID)
	if err != nil {
		t.Fatal(err)
	}
	if welcome.Scene.Name != info.Name {
		t.Fatalf("握手的世界名 %q", welcome.Scene.Name)
	}
	// 存档连接只接受同源页面。
	foreign, _, err := websocket.Dial(context.Background(), fmt.Sprintf("ws%s/r2link?world=%d", strings.TrimPrefix(server.URL, "http"), info.ID), &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": {"http://evil.example"}},
	})
	if err == nil {
		foreign.CloseNow()
		t.Fatal("接受了别的页面来源")
	}
	// 退订再订阅时命中缓存，内容不变。
	region := func() []byte {
		t.Helper()
		var header r2link.RegionData
		frame := client.receive(&header)
		if header.Type != "region" || header.X != 0 || header.Z != 0 || len(frame.Tail) == 0 {
			t.Fatalf("区域回复 %+v", header)
		}
		return frame.Tail
	}
	client.want([]r2link.RegionPos{{}}, nil, nil)
	first := region()
	client.want(nil, []r2link.RegionPos{{}}, nil)
	client.want([]r2link.RegionPos{{}}, nil, nil)
	if !bytes.Equal(first, region()) {
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
	objects := func(center [3]int) []object.Descriptor {
		t.Helper()
		client.want(nil, nil, []r2link.ObjectWant{{Center: center, Radius: 8}})
		var reply r2link.Objects
		client.receive(&reply)
		if reply.Type != "objects" || reply.X != 0 || reply.Z != 0 {
			t.Fatalf("对象回复 %+v", reply)
		}
		return reply.Descriptors
	}
	if near := objects([3]int{}); len(near) != 2 || actor.calls != 1 || actor.text != "牌面内容" {
		t.Fatalf("近景扩展失败：%+v %+v", near, actor)
	}
	if far := objects([3]int{100, 0, 0}); len(far) != 0 || actor.calls != 1 {
		t.Fatal("近景范围外仍生成了特殊对象")
	}
	// 关闭存档断开旧连接，旧编号不能再连接。
	request("POST", "/api/world/close", "")
	if frame, err := client.next(); err == nil {
		t.Fatalf("关闭后连接仍在：%s", frame.Type)
	}
	if _, _, err = dialWorld(t, server, info.ID); err == nil {
		t.Fatal("关闭后仍接受旧会话连接")
	}
	reopened, err := service.Open(context.Background(), OpenOptions{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = dialWorld(t, server, info.ID); reopened.ID <= info.ID || err == nil {
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
	service := New(nil, nil)
	defer service.Close()
	info, err := service.Open(context.Background(), OpenOptions{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(service.Handler(fstest.MapFS{}))
	defer server.Close()
	client, _, err := dialWorld(t, server, info.ID)
	if err != nil {
		t.Fatal(err)
	}
	data := client.chunks()
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
	// 新增区域后当前会话仍应命中缓存，重新打开才更新地图。Windows 上存档的已开文件会被锁住。
	region, err := os.ReadFile(filepath.Join(path, "region", "r.0.0.mca"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(path, "region", "r.9.9.mca"), region, 0600); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(client.chunks(), data) {
		t.Fatal("占用表未命中会话缓存")
	}
	reopened, err := service.Open(context.Background(), OpenOptions{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = dialWorld(t, server, info.ID); err == nil {
		t.Fatal("切换后仍接受旧会话的连接")
	}
	again, _, err := dialWorld(t, server, reopened.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated := again.chunks(); len(updated) != len(data)+136 {
		t.Fatal("重新打开后占用表应包含新增区域")
	}
	service.Close()
	if _, _, err = dialWorld(t, server, reopened.ID); err == nil {
		t.Fatal("关闭后仍接受旧会话的连接")
	}
}

// 内置场景与存档走同一条 R2Link 路径，关闭后只更换会话编号，不改变场景内容。
func TestMobiusSession(t *testing.T) {
	service := New(nil, nil)
	defer service.Close()
	handler := service.Handler(fstest.MapFS{})
	server := httptest.NewServer(handler)
	defer server.Close()
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
	var err error
	if err = json.Unmarshal(request("/api/world").Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if !info.Loaded || info.Name != "Möbius" || info.Scene == nil || info.Path != "" {
		t.Fatalf("默认场景信息错误：%+v", info)
	}
	if info.Scene.Ocean != 64 || info.Scene.Camera.Position != info.Position || info.Scene.Time != 19 {
		t.Fatalf("开场设置错误：%+v", info.Scene)
	}
	client, _, err := dialWorld(t, server, info.ID)
	if err != nil {
		t.Fatal(err)
	}
	occupied := make(map[[2]int]bool)
	chunks := client.chunks()
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
	client.want([]r2link.RegionPos{{X: -1, Z: -1}, {X: -1, Z: 0}, {X: 0, Z: -1}, {X: 0, Z: 0}}, nil, nil)
	for range 4 {
		var header r2link.RegionData
		data := client.receive(&header).Tail
		if header.Type != "region" {
			t.Fatalf("区域回复 %+v", header)
		}
		words := make([]uint32, len(data)/4)
		if err = binary.Read(bytes.NewReader(data), binary.LittleEndian, words); err != nil {
			t.Fatal(err)
		}
		if words[0] != r2link.RegionMagic || words[1] == 0 {
			t.Fatalf("区域头错误：%v", words[:2])
		}
		cursor := 2
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
						if model == r2link.AirHash {
							continue
						}
						if model != r2link.BlockHash("minecraft:sea_lantern []") {
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
	if lanterns < 1000 {
		t.Fatalf("环没有完整生成：%d 格", lanterns)
	}
	client.want(nil, nil, []r2link.ObjectWant{{Center: [3]int{0, 4, 0}, Radius: 8}})
	var objects r2link.Objects
	if client.receive(&objects); objects.Type != "objects" || len(objects.Descriptors) != 0 {
		t.Fatal("内置场景应只有静态方块")
	}
	service.Close()
	closed := service.Info()
	if closed.ID <= info.ID || closed.Name != info.Name || *closed.Scene != *info.Scene {
		t.Fatalf("关闭后没有恢复默认场景：%+v", closed)
	}
}
