"""按本应用的 versions.json 重建发布时嵌入的全部 Minecraft 资源。"""

import json
from pathlib import Path
import runpy
import tempfile
from zipfile import ZipFile, ZipInfo

HERE = Path(__file__).resolve().parent
WORKSPACE = HERE.parents[2]
CORE = WORKSPACE / "neomega-converter-core/resourcegen"
R2 = WORKSPACE / "neomega-r2/assets-utils/resourcegen"


def generate() -> None:
    versions = json.loads((HERE / "versions.json").read_text())
    java = versions["blocks"]["java"]
    with tempfile.TemporaryDirectory(dir=HERE) as directory:
        work = Path(directory)
        # 区块、结构和显示资源使用同一套方块身份，几何直接复用本次生成的状态。
        tables = work / "tables"
        build_tables = runpy.run_path(str(CORE / "world_tables.py"))["generate"]
        states = build_tables(
            java=java,
            bedrock=versions["blocks"]["bedrock"],
            mappings=versions["mappings"],
            groups={"bedrock-to-java-chunk", "structure", "java-block-states"},
            out=tables,
            work=work,
        )
        build_render_assets = runpy.run_path(str(R2 / "generate.py"))["generate"]
        render = build_render_assets(
            java=java,
            topics={"solid-faces", "browser-materials"},
            blocks=states,
            out=work / "render",
        )
        # 每次构建全新的包，生成中途失败时仍可使用原来的整套资源。
        with ZipFile(work / "minecraft.zip", "w") as archive:
            for path in sorted(tables.iterdir()):
                archive.writestr(ZipInfo("tables/" + path.name), path.read_bytes())
            for name in (
                "solid-faces/solid-faces.nvsolid",
                "browser-materials/default-materials.zip",
                "browser-materials/material-info.zip",
                "browser-materials/THIRD_PARTY_NOTICES.txt",
            ):
                archive.writestr(ZipInfo(name), (render / name).read_bytes())
        (work / "minecraft.zip").replace(HERE / "minecraft.zip")


if __name__ == "__main__":
    generate()
