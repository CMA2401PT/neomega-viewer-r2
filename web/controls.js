/** 相机输入只管理姿态；移动后的区块调度由应用的帧循环决定。 */
export class FlightControls {
  constructor(canvas, camera) {
    this.camera = camera;
    this.keys = new Set();
    this.canvas = canvas;
    window.addEventListener("keydown", (e) => {
      if (
        e.target.closest("input,select,textarea") ||
        document.querySelector("dialog[open]")
      )
        return;
      this.keys.add(e.key.toLowerCase());
      if ([" ", "ArrowUp", "ArrowDown"].includes(e.key)) e.preventDefault();
    });
    window.addEventListener("keyup", (e) =>
      this.keys.delete(e.key.toLowerCase()),
    );
    window.addEventListener("blur", () => this.keys.clear());
    canvas.addEventListener("pointerdown", (e) => {
      canvas.focus();
      canvas.setPointerCapture(e.pointerId);
    });
    canvas.addEventListener("pointermove", (e) => {
      if (!canvas.hasPointerCapture(e.pointerId)) return;
      camera.yaw += e.movementX * 0.004;
      camera.pitch = Math.max(
        -1.5,
        Math.min(1.5, camera.pitch - e.movementY * 0.004),
      );
    });
    canvas.addEventListener("pointerup", (e) =>
      canvas.releasePointerCapture(e.pointerId),
    );
  }
  update(dt, speed) {
    if (document.querySelector("dialog[open]")) {
      this.keys.clear();
      return;
    }
    const k = this.keys,
      c = this.camera,
      s = dt * speed * (k.has("shift") ? 3 : 1),
      forward = [
        Math.sin(c.yaw) * Math.cos(c.pitch),
        Math.sin(c.pitch),
        -Math.cos(c.yaw) * Math.cos(c.pitch),
      ],
      right = [Math.cos(c.yaw), 0, Math.sin(c.yaw)];
    for (let a = 0; a < 3; a++)
      c.position[a] +=
        s *
        ((Number(k.has("w")) - Number(k.has("s"))) * forward[a] +
          (Number(k.has("d")) - Number(k.has("a"))) * right[a]);
    c.position[1] +=
      s * (Number(k.has("e") || k.has(" ")) - Number(k.has("q")));
  }
}
