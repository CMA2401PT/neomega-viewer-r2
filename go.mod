module neomega-viewer-r2

go 1.25.5

require (
	celler v0.0.0
	github.com/coder/websocket v1.8.15
	neomega-blocks v0.0.0
	neomega-converter v0.0.0
	neomega-converter-core v0.0.0
	neomega-nbt v0.0.0
	neomega-r2 v0.0.0
	neomega-view-utils v0.0.0
)

require (
	github.com/df-mc/goleveldb v1.1.9 // indirect
	github.com/golang/snappy v0.0.1 // indirect
	github.com/klauspost/compress v1.13.6 // indirect
	github.com/pierrec/lz4/v4 v4.1.22 // indirect
	neomega-packfs v0.0.0 // indirect
)

replace celler => ../../neomega-celler

replace neomega-blocks => ../../neomega-blocks

replace neomega-nbt => ../../neomega-nbt

replace neomega-packfs => ../../neomega-packfs

replace neomega-r2 => ../../neomega-r2

replace neomega-converter-core => ../../neomega-converter-core

replace neomega-converter => ../neomega-converter

replace neomega-view-utils => ../../neomega-view-utils
