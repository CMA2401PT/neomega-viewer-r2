// 主相机的键盘、拖动与滚轮输入；屏幕按钮和键盘共用方向状态。
/** @typedef {"forward"|"back"|"left"|"right"|"up"|"down"} Move */
/** @type {Record<string, Move>} */
const KEY_MOVES = {
  KeyW: "forward",
  KeyS: "back",
  KeyA: "left",
  KeyD: "right",
  Space: "up",
  ShiftLeft: "down",
  ShiftRight: "down",
};

/** 水平移动只用 yaw，升降独立沿 Y 轴；滚轮沿完整视线步进。 */
export class FlightControls {
  /** @param {HTMLCanvasElement} canvas
   * @param {{position:number[],yaw:number,pitch:number}} camera
   */
  constructor(canvas, camera) {
    this.enabled = true;
    this.camera = camera;
    this.canvas = canvas;
    /** @type {Map<string, Move>} */
    this.held = new Map();
    /** @type {{id:number,x:number,y:number}|null} */
    this.drag = null;
    window.addEventListener("keydown", (event) => {
      const move = KEY_MOVES[event.code];
      if (
        !move ||
        this.paused ||
        event.target.closest("input,select,textarea,button")
      )
        return;
      event.preventDefault();
      this.held.set(event.code, move);
    });
    window.addEventListener("keyup", (event) => this.held.delete(event.code));
    window.addEventListener("blur", () => this.cancel());
    canvas.addEventListener("blur", () => {
      for (const code of Object.keys(KEY_MOVES)) this.held.delete(code);
    });
    canvas.addEventListener("pointerdown", (event) => {
      if (
        !this.enabled ||
        this.drag ||
        (event.button !== 0 && event.button !== 2)
      )
        return;
      canvas.focus();
      if (this.paused) return;
      this.drag = { id: event.pointerId, x: event.clientX, y: event.clientY };
      canvas.setPointerCapture(event.pointerId);
    });
    canvas.addEventListener("pointermove", (event) => {
      const drag = this.drag;
      if (!drag || drag.id !== event.pointerId || this.paused) return;
      camera.yaw += (event.clientX - drag.x) * 0.004;
      camera.pitch = Math.max(
        -Math.PI / 2 + 0.01,
        Math.min(
          Math.PI / 2 - 0.01,
          camera.pitch - (event.clientY - drag.y) * 0.004,
        ),
      );
      drag.x = event.clientX;
      drag.y = event.clientY;
    });
    /** @param {PointerEvent} event */
    const release = (event) => {
      if (this.drag?.id === event.pointerId) this.drag = null;
    };
    canvas.addEventListener("pointerup", release);
    canvas.addEventListener("pointercancel", release);
    canvas.addEventListener("lostpointercapture", release);
    canvas.addEventListener("contextmenu", (event) => event.preventDefault());
    canvas.addEventListener(
      "wheel",
      (event) => {
        event.preventDefault();
        if (this.paused) return;
        const step = -Math.sign(event.deltaY) * (event.shiftKey ? 32 : 8);
        const forward = [
          Math.sin(camera.yaw) * Math.cos(camera.pitch),
          Math.sin(camera.pitch),
          -Math.cos(camera.yaw) * Math.cos(camera.pitch),
        ];
        camera.position = camera.position.map((v, i) => v + forward[i] * step);
      },
      { passive: false },
    );
  }
  /** 弹窗和地图暂停主相机，避免一次操作同时作用于两个视图。 */
  get paused() {
    return !this.enabled || Boolean(document.querySelector("dialog[open]"));
  }
  /** 屏幕方向与对应按键效果相同，松开按钮不影响同方向的键盘输入。
   * @param {{move:Move,down:boolean}} options
   */
  hold({ move, down }) {
    const source = `pad:${move}`;
    if (!down) this.held.delete(source);
    else if (!this.paused) this.held.set(source, move);
  }
  /** 清空持续输入，切换地图或窗口失焦后不会继续移动。 */
  cancel() {
    this.held.clear();
    this.drag = null;
  }
  /** 按当前速度推进水平和垂直位移，抬头低头不改变水平移动高度。
   * @param {number} dt @param {number} speed
   */
  update(dt, speed) {
    if (
      this.paused ||
      document.activeElement.closest("input,select,textarea")
    ) {
      this.cancel();
      return;
    }
    const moves = new Set(this.held.values());
    /** @param {Move} plus @param {Move} minus */
    const axis = (plus, minus) =>
      Number(moves.has(plus)) - Number(moves.has(minus));
    const c = this.camera,
      step = dt * speed;
    const forward = axis("forward", "back"),
      right = axis("right", "left");
    c.position[0] +=
      step * (Math.sin(c.yaw) * forward + Math.cos(c.yaw) * right);
    c.position[2] +=
      step * (-Math.cos(c.yaw) * forward + Math.sin(c.yaw) * right);
    c.position[1] += step * axis("up", "down");
  }
}
