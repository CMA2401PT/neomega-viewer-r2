package r2link

import (
	"celler/layer_storage"
	"encoding/binary"
	"neomega-blocks/ir"
	"testing"
)

// 非二次幂调色板覆盖不跨字的三位索引，负坐标与双层顺序一起往返。
func TestMessagePaletteRoundTrip(t *testing.T) {
	storage := layer_storage.MakePaletteStorage(ir.IRBlock("minecraft:air []"))
	for i := 0; i < 4096; i++ {
		storage.Set(layer_storage.At(i>>8, i&15, i>>4), []ir.IRBlock{"minecraft:air []", "minecraft:stone []", "minecraft:dirt []", "minecraft:glass []", "minecraft:gold_block []"}[i%5])
	}
	layer := FromStorage(storage)
	message := Message{Type: "chunk", Mode: "patch", X: -1, Z: 2, Sections: []Section{{Y: -4, Layers: []Layer{layer, {Palette: []string{"minecraft:air []"}}}}}}
	data, err := Encode(message)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	result := decoded.Sections[0].Layers[0].Storage()
	for i := 0; i < 4096; i++ {
		pos := layer_storage.At(i>>8, i&15, i>>4)
		if result.Get(pos) != storage.Get(pos) {
			t.Fatal(i)
		}
	}
	if _, err := Decode(data[:len(data)-1]); err == nil {
		t.Fatal("接受截断索引")
	}
	start := 4 + int(binary.LittleEndian.Uint32(data))
	data[start] = 7
	if _, err := Decode(data); err == nil {
		t.Fatal("接受越界调色板索引")
	}
}
