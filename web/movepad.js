// 触屏按钮只管理按住与松开的生命周期，移动方向和速度交给飞行控制器。
/** 指针捕获允许手指按住按钮的同时用另一根手指拖动场景。
 * @param {HTMLElement} pad
 * @param {import("./controls.js").FlightControls} controls
 */
export function bindMovePad(pad, controls) {
  for (const button of pad.querySelectorAll("[data-move]")) {
    const move = /** @type {import("./controls.js").Move} */ (
      button.dataset.move
    );
    /** 系统取消或失去捕获时也松开，避免方向键粘住。 */
    const release = () => {
      controls.hold({ move, down: false });
      delete button.dataset.held;
    };
    button.addEventListener("pointerdown", (event) => {
      event.preventDefault();
      controls.canvas.focus();
      button.setPointerCapture(event.pointerId);
      button.dataset.held = "";
      controls.hold({ move, down: true });
    });
    button.addEventListener("pointerup", release);
    button.addEventListener("pointercancel", release);
    button.addEventListener("lostpointercapture", release);
    button.addEventListener("contextmenu", (event) => event.preventDefault());
    window.addEventListener("blur", release);
  }
}
