package worlddata

import (
	"neomega-blocks/ir"
	blocks "neomega-blocks/presets/java/v1"
	"neomega-view-utils/r2assets"
)

// StateMapping 在应用启动时共享建立状态映射，区域请求仅转换调色板条目。
// 含水状态目前只发布方块本体，附加水层尚未接入。
func StateMapping(catalog *r2assets.Catalog) map[string]uint32 {
	mapping := make(map[string]uint32)
	for state := range blocks.JavaCompatibleStates() {
		normalized, ok := blocks.NormalizeSingleLayer(state)
		if !ok {
			continue
		}
		model, ok := catalog.States[string(normalized)]
		if !ok {
			continue
		}
		mapping[string(state)] = model
		for _, value := range []string{"false", "true"} {
			variant := ir.IRBlock(state.NameWithNameSpace() + " " + string(state.States().Set("waterlogged", value))).Tidy()
			mapping[string(variant)] = model
		}
	}
	return mapping
}
