package linksource

import (
	"context"
	"encoding/binary"
	"neomega-viewer-r2/r2link"
	"sync"
	"testing"
)

// 单色层让测试聚焦提交语义，不把网络打包细节混进内存世界测试。
func section(y int32, state string) r2link.Section {
	return r2link.Section{Y: y, Layers: []r2link.Layer{{Palette: []string{state}}}}
}

// patch 从空气起步，replace 清掉未列出高度，删除与预算失败都不能留下半份提交。
func TestCommitModesAndBudget(t *testing.T) {
	welcome := r2link.Message{Blocks: "java", Delivery: "push", Scene: &r2link.Scene{Dimension: ".", MinY: -64, Height: 384}}
	world, err := New(welcome, false, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range []r2link.Message{
		{Mode: "patch", Sections: []r2link.Section{section(0, "minecraft:stone []"), section(28, "minecraft:stone []")}},
		{Mode: "patch", DeleteSections: []int32{0}},
	} {
		if _, err := world.Apply(message); err != nil {
			t.Fatal(err)
		}
	}
	if stats := world.Statistics(); stats.ClippedSections != 1 || stats.Chunks != 1 {
		t.Fatal(stats)
	}
	if _, err := world.Apply(r2link.Message{Mode: "replace", Sections: []r2link.Section{section(1, "minecraft:stone []")}}); err != nil {
		t.Fatal(err)
	}
	before := world.Statistics()
	world.limit = before.Bytes
	if _, err := world.Apply(r2link.Message{Mode: "patch", X: 1, Sections: []r2link.Section{section(0, "minecraft:stone []")}}); err == nil {
		t.Fatal("未执行预算")
	}
	if world.Statistics() != before {
		t.Fatal("预算失败修改了世界")
	}
	data, err := world.Region(context.Background(), 0, 0)
	if err != nil || binary.LittleEndian.Uint32(data[4:]) != 1 {
		t.Fatal(data, err)
	}
	if _, err := world.Apply(r2link.Message{Mode: "delete"}); err != nil {
		t.Fatal(err)
	}
	if world.Statistics().Chunks != 0 {
		t.Fatal(world.Statistics())
	}
}

// 并发读取只能看到完整快照；关闭与提交交错也必须保持内存访问安全。
func TestConcurrentCommitReadClose(t *testing.T) {
	world, err := New(r2link.Message{Blocks: "java", Delivery: "push", Scene: &r2link.Scene{Height: 384}}, false, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	var tasks sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		tasks.Add(1)
		go func(worker int) {
			defer tasks.Done()
			for i := 0; i < 30; i++ {
				switch worker {
				case 0:
					world.Apply(r2link.Message{Mode: "replace", Sections: []r2link.Section{section(0, "minecraft:stone []")}})
				case 1:
					world.Region(context.Background(), 0, 0)
				case 2:
					world.Chunks(context.Background())
					world.Statistics()
				case 3:
					world.Close()
				}
			}
		}(worker)
	}
	tasks.Wait()
}

// Bedrock 双层转换保留含水；邻居到达和删除都重新修复连接属性。
func TestBedrockLayersAndNeighbours(t *testing.T) {
	world, err := New(r2link.Message{Blocks: "bedrock", Delivery: "push", Scene: &r2link.Scene{Dimension: "0", Height: 384}}, true, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	fence := section(0, "minecraft:oak_fence []")
	fence.Layers = append(fence.Layers, r2link.Layer{Palette: []string{`minecraft:water ["liquid_depth"=0]`}})
	if _, err := world.Apply(r2link.Message{Mode: "replace", Sections: []r2link.Section{fence}}); err != nil {
		t.Fatal(err)
	}
	if _, err := world.Apply(r2link.Message{Mode: "replace", X: 1, Sections: []r2link.Section{section(0, "minecraft:stone []")}}); err != nil {
		t.Fatal(err)
	}
	keys, err := world.Retain([]r2link.Range{{MinX: 1, MaxX: 1}})
	if err != nil || len(keys) == 0 || world.Statistics().Chunks != 1 {
		t.Fatal(keys, err, world.Statistics())
	}
}
