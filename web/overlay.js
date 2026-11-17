// 场景读数使用独立 2D 坐标轴，不参与主画布的射线绘制或照片输出。
import { cameraBasis } from "/r2/index.js";

/** 相机三条轴投影到屏幕，按深度排序让靠近观察者的轴盖住远轴。
 * @param {HTMLCanvasElement} canvas
 * @param {{yaw:number,pitch:number}} camera
 */
export function drawAxes(canvas, camera) {
  const ratio = devicePixelRatio,
    size = 84;
  if (canvas.width !== size * ratio)
    canvas.width = canvas.height = size * ratio;
  const ctx = canvas.getContext("2d"),
    { forward, right, up } = cameraBasis(camera);
  ctx.setTransform(ratio, 0, 0, ratio, 0, 0);
  ctx.clearRect(0, 0, size, size);
  ctx.lineWidth = 2;
  ctx.lineCap = "round";
  ctx.font = "600 10px system-ui";
  ctx.textAlign = "center";
  ctx.textBaseline = "middle";
  const axes = ["X", "Y", "Z"]
    .flatMap((label, i) =>
      [1, -1].map((sign) => ({
        x: right[i] * sign,
        y: up[i] * sign,
        depth: -forward[i] * sign,
        color: ["#d1453b", "#3f9a4b", "#3a6fc4"][i],
        label: sign === 1 ? label : "",
      })),
    )
    .sort((a, b) => a.depth - b.depth);
  for (const { x, y, color, label } of axes) {
    const px = 42 + x * 30,
      py = 42 - y * 30;
    ctx.strokeStyle = ctx.fillStyle = color;
    ctx.shadowColor = "#fff";
    ctx.shadowBlur = 3;
    if (label) {
      ctx.beginPath();
      ctx.moveTo(42, 42);
      ctx.lineTo(px, py);
      ctx.stroke();
      ctx.beginPath();
      ctx.arc(px, py, 8, 0, Math.PI * 2);
      ctx.fill();
      ctx.shadowBlur = 0;
      ctx.fillStyle = "#fff";
      ctx.fillText(label, px, py + 0.5);
    } else {
      ctx.globalAlpha = 0.35;
      ctx.beginPath();
      ctx.arc(px, py, 5, 0, Math.PI * 2);
      ctx.fill();
      ctx.globalAlpha = 1;
    }
  }
}
