// Package worldsource 把原生存档与转换修复接到 R2 的公共区域适配器。
package worldsource

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"neomega-converter-core/conversiondata"
	fixer "neomega-view-utils/fixer/java-postprocess"
	"slices"

	"celler"
	"celler/java"
	converter "neomega-converter-core"
	"neomega-converter-core/be2je/actors"
	"neomega-converter-core/be2je/adapt"
	"neomega-converter-core/worldinput"
	fixedadapt "neomega-converter/adapt"
	nbt "neomega-nbt/presets/v1"

	"neomega-viewer-r2/internal/worlddata"
)

// Opener 在同一组发布资源上打开不同存档，按输入格式取用转换表或修复器。
type Opener struct {
	Tables *conversiondata.Loader
	Fixer  func() (*fixer.Fixer, error)
}

// Options 把原生打开选项和显示用的邻接修复分开，Java 状态不经过 Bedrock 修复。
type Options struct {
	worldinput.Options
	SkipBedrockFix bool
}

// World 拥有文件来源，渲染适配器只借用它提供的只读 Java 区块。
type World struct {
	*worlddata.World
	Input *worldinput.Source
}

// Open 在发布会话前完成原生打开与修复器准备，失败时保留调用方的旧世界。
func (o Opener) Open(ctx context.Context, options Options) (*World, error) {
	input, err := worldinput.Open(ctx, o.Tables.Schema, o.Tables.Structure, options.Options)
	if err != nil {
		return nil, err
	}
	output := &worlddata.World{}
	if input.Java != nil {
		schema, err := o.Tables.Schema()
		if err != nil {
			input.Close()
			return nil, err
		}
		output.ScanChunks = input.Java.Chunks
		output.ReadEntities = func(ctx context.Context, pos celler.ChunkPos) ([]nbt.Compound, error) {
			records, err := input.Java.Read(ctx, pos)
			if err != nil {
				return nil, err
			}
			record := records[1]
			// 旧存档把 Entities 保存于地形记录，独立实体记录优先。
			if record == nil {
				record = records[0]
			}
			if record == nil {
				return nil, nil
			}
			raw, err := record.Decode()
			if err != nil {
				return nil, err
			}
			return java.DecodeEntities(raw)
		}
		readJava := func(ctx context.Context, pos celler.ChunkPos) (*java.Chunk, error) {
			records, err := input.Java.Read(ctx, pos)
			if err != nil || records[0] == nil {
				return nil, err
			}
			raw, err := records[0].Decode()
			if err != nil {
				return nil, err
			}
			return java.DecodeChunk(raw, schema)
		}
		output.ReadBlockEntities = func(ctx context.Context, pos celler.ChunkPos) ([]java.BlockEntity, error) {
			chunk, err := readJava(ctx, pos)
			if err != nil || chunk == nil {
				return nil, err
			}
			entities, _, err := chunk.BlockEntities()
			return entities, err
		}
		output.ReadChunk = func(ctx context.Context, pos celler.ChunkPos) (*converter.JavaChunk, error) {
			chunk, err := readJava(ctx, pos)
			if err != nil || chunk == nil {
				return nil, err
			}
			ys, err := chunk.SectionYs()
			if err != nil {
				return nil, err
			}
			result := &converter.JavaChunk{Pos: pos, Sections: map[int32]*converter.JavaSection{}}
			for _, y := range ys {
				section, err := chunk.Section(y)
				if err != nil {
					return nil, err
				}
				result.Sections[y] = &converter.JavaSection{Blocks: section.Blocks}
			}
			return result, nil
		}
	} else {
		reads := input.Bedrock
		// 直接读取 Bedrock 方块实体，保留转换器未识别的字段及方块种类。
		output.ReadBlockEntities = func(ctx context.Context, pos celler.ChunkPos) ([]java.BlockEntity, error) {
			chunk, err := reads.Read(ctx, pos)
			if errors.Is(err, fs.ErrNotExist) {
				return nil, nil
			}
			if err != nil || chunk == nil {
				return nil, err
			}
			documents, err := chunk.BlockEntities()
			if err != nil {
				return nil, err
			}
			entities := make([]java.BlockEntity, 0, len(documents))
			for _, document := range documents {
				value := document.Value
				x, xOK := value["x"].(int32)
				y, yOK := value["y"].(int32)
				z, zOK := value["z"].(int32)
				if !xOK || !yOK || !zOK {
					return nil, fmt.Errorf("区块 %v 的方块实体缺少整数坐标", pos)
				}
				entities = append(entities, java.BlockEntity{Pos: celler.BlockPos{X: x, Y: y, Z: z}, NBT: value})
			}
			return entities, nil
		}
		rules, err := o.Tables.BedrockToJavaChunk()
		if err != nil {
			input.Close()
			return nil, err
		}
		converted := adapt.NewConverted(rules, reads.Read, adapt.Options{})
		output.ReadEntities = func(ctx context.Context, pos celler.ChunkPos) ([]nbt.Compound, error) {
			chunk, err := converted.Chunk(ctx, pos)
			if err != nil || chunk == nil {
				return nil, err
			}
			// 较新版存档将活动实体独立存储，只在近景订阅中读取。
			extra, err := reads.ReadActors(ctx, pos)
			if err != nil || len(extra) == 0 {
				return chunk.Entities, err
			}
			entities := slices.Clone(chunk.Entities)
			for _, raw := range extra {
				if entity, _ := actors.Convert(rules.Entities, rules.Items, raw); entity != nil {
					entities = append(entities, entity)
				}
			}
			return entities, nil
		}
		var chunks adapt.ChunkSource = converted
		if !options.SkipBedrockFix {
			f, err := o.Fixer()
			if err != nil {
				input.Close()
				return nil, err
			}
			chunks = fixedadapt.NewFixed(converted, f, adapt.Options{})
		}
		output.ReadChunk = chunks.Chunk
		output.ScanChunks = reads.Chunks
	}
	return &World{World: output, Input: input}, nil
}

// Close 将渲染请求归还的来源交回原生读取层，由它等待底层 I/O 并关闭句柄。
func (w *World) Close() error {
	return w.Input.Close()
}
