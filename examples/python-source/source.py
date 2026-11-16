"""R2Link 最小 TCP 数据源，只依赖 Python 标准库。

运行：python3 apps/neomega-viewer-r2/examples/python-source/source.py
在 R2 的连接面板输入 tcp://127.0.0.1:2415。
数据源监听，R2 主动连接；每次连接都重新 hello/welcome/ready，再发送小场景。
"""

from __future__ import annotations

import argparse
import json
import math
import socket
import struct
from typing import Any, BinaryIO


# TCP 是字节流，外层长度用于读取一条完整消息；内层长度分开 JSON 和索引。
def receive(stream: BinaryIO) -> dict[str, Any]:
    prefix: bytes = stream.read(4)
    if not prefix:
        raise EOFError
    size: int = struct.unpack("<I", prefix)[0]
    body: bytes = stream.read(size)
    header_size: int = struct.unpack_from("<I", body)[0]
    return json.loads(body[4 : 4 + header_size])


# 控制消息的 words 为空；section 索引按小端 uint32 紧随 JSON。
def send(
    connection: socket.socket,
    header: dict[str, Any],
    words: list[int] | None = None,
) -> None:
    encoded: bytes = json.dumps(
        header,
        ensure_ascii=False,
        separators=(",", ":"),
    ).encode("utf-8")
    body: bytes = struct.pack("<I", len(encoded)) + encoded
    if words:
        body += struct.pack(f"<{len(words)}I", *words)
    connection.sendall(struct.pack("<I", len(body)) + body)


# 每个 section 有 4096 格，局部序号为 x*256+z*16+y；同一索引不能跨 uint32。
def scene_section() -> tuple[dict[str, Any], list[int]]:
    palette: list[str] = [
        "minecraft:air []",
        "minecraft:stone []",
        "minecraft:gold_block []",
        "minecraft:sea_lantern []",
    ]
    bits: int = (len(palette) - 1).bit_length()
    per_word: int = 32 // bits
    words: list[int] = [0] * math.ceil(4096 / per_word)
    for x in range(16):
        for z in range(16):
            for y in range(16):
                index: int = 1 if y == 0 else 0
                if x in (4, 11) and z in (4, 11) and 1 <= y <= 6:
                    index = 3 if y == 6 else 2
                offset: int = x * 256 + z * 16 + y
                words[offset // per_word] |= index << ((offset % per_word) * bits)
    section: dict[str, Any] = {"y": 0, "layers": [{"palette": palette, "bits": bits}]}
    return section, words


# push 模式适合一次性的小场景，仍读取 interest，使对端写入不被堵住。
def serve(connection: socket.socket) -> None:
    with connection, connection.makefile("rb") as stream:
        hello: dict[str, Any] = receive(stream=stream)
        if hello["type"] != "hello" or hello["version"] != 1:
            raise ValueError("需要 R2Link v1 hello")
        send(
            connection=connection,
            header={
                "type": "welcome", "protocol": "r2link", "version": 1,
                "blocks": "java", "delivery": "push",
                "scene": {"name": "Python 小场景", "dimension": ".", "minY": -64, "height": 384},
                "camera": {"position": [25, 22, 30], "yaw": -0.66, "pitch": -0.55},
            },
        )
        ready: dict[str, Any] = receive(stream=stream)
        if ready["type"] != "ready":
            raise ValueError("需要 ready 后才能发送场景")
        section: dict[str, Any]
        words: list[int]
        section, words = scene_section()
        send(
            connection=connection,
            header={
                "type": "chunk", "mode": "replace", "x": 0, "z": 0, "sections": [section],
            },
            words=words,
        )
        while True:
            message: dict[str, Any] = receive(stream=stream)
            if message["type"] == "error":
                raise ValueError(message["message"])


# 串行接受连接足够演示重新握手；异常连接结束后继续监听下一次重连。
def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", type=int, default=2415)
    options: argparse.Namespace = parser.parse_args()
    with socket.socket() as listener:
        listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        listener.bind((options.host, options.port))
        listener.listen()
        print(f"R2 数据源：tcp://{options.host}:{options.port}", flush=True)
        while True:
            connection: socket.socket
            connection, _ = listener.accept()
            try:
                serve(connection=connection)
            except (EOFError, ConnectionError) as error:
                print(f"连接结束：{error}", flush=True)


if __name__ == "__main__":
    main()
