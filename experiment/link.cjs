// 用可控数据源验证真实 TCP/WS、SSE 重拉、GPU 替换和相机控制。
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || "/Applications/ChatGPT.app/Contents/Resources/cua_node/lib/node_modules/playwright");
const fs = require("node:fs"), assert = require("node:assert/strict");
const base = process.env.R2_URL || "http://127.0.0.1:2417";
const fixture = process.env.R2_LINK_FIXTURE || "http://127.0.0.1:2428";
const out = process.env.R2_OUTPUT || "/tmp/r2link/link";
fs.mkdirSync(out, {recursive:true});

/** 测试构造真实消息体，控制接口只把它原样发进 TCP/WS。 */
function encode(header, words = []) {
  const json = Buffer.from(JSON.stringify(header));
  const body = Buffer.alloc(4 + json.length + words.length * 4);
  body.writeUInt32LE(json.length); json.copy(body, 4);
  words.forEach((word, i) => body.writeUInt32LE(word, 4 + json.length + i * 4));
  return body;
}
/** 均匀 section 的 bits 为零，删除测试不会受到压缩实现的干扰。 */
function section(y, state) { return {y, layers:[{palette:[`minecraft:${state} []`], bits:0}]}; }
/** HTTP 控制失败直接结束测试，避免把未送达的更新误当成渲染错误。 */
async function send(message) {
  const response = await fetch(fixture + "/send", {method:"POST",body:encode(message)});
  assert(response.ok, await response.text());
}
(async () => {
  const browser = await chromium.launch({executablePath:process.env.CHROME || "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",headless:true,args:["--enable-webgl","--ignore-gpu-blocklist","--use-angle=metal"]});
  const result = {errors:[],checks:[]};
  try {
    const page = await browser.newPage({viewport:{width:1280,height:720},deviceScaleFactor:1});
    page.on("pageerror", error => result.errors.push(String(error)));
    page.on("console", message => {if (/GL_INVALID|WebGL:.*INVALID/.test(message.text())) result.errors.push(message.text());});
    await page.request.post(base + "/api/world/close");
    await page.goto(base);
    await page.waitForFunction(() => globalThis.viewerR2);
    assert.equal(await page.locator("#connect-source svg").count(), 1);
    assert.equal((await page.locator("#connect-source").textContent()).trim(), "");
    assert.equal(await page.locator("#connect-source").getAttribute("title"), "连接数据源");
    assert.equal(await page.locator("#link-banner, #link-disconnect").count(), 0);
    await page.click("#connect-source");
    await page.screenshot({path:`${out}/connection-dialog.png`});
    await page.click("#link-dismiss");
    assert.equal(await page.locator("#link-dialog").isVisible(), false);
    for (const [id,value] of [["far-radius",16],["near-radius",1],["still-samples",0],["time",10]]) {
      await page.locator("#"+id).evaluate((el,value)=>{el.value=String(value);el.dispatchEvent(new Event(el.id==="time"?"input":"change"));},value);
    }
    await page.click("#time-flow");
    const settled = async () => {
      await page.waitForTimeout(150);
      await page.waitForFunction(()=>{const s=viewerR2.state.stream;return s.loaded===s.wanted && !s.pending && !s.nearPending && !s.failed;},null,{timeout:120000});
      assert.equal(await page.evaluate(()=>viewerR2.state.error),0);
    };
    const pick = async (x=8.5,z=8.5) => page.evaluate(([x,z])=>viewerR2.renderer.pick({position:[x,200,z],yaw:0,pitch:-Math.PI/2,ortho:16},{width:101,height:101,x:50,y:50}),[x,z]);
    const pixels = async () => page.evaluate(()=>{
      const r=viewerR2.renderer;r.draw(viewerR2.state.camera,{width:1280,height:720,time:0,objects:false,sun:[0,1,0],fog:Infinity});
      const p=r.pixels();let text="";for(let i=0;i<p.length;i+=8192)text+=String.fromCharCode(...p.subarray(i,i+8192));return btoa(text);
    });
    const updated = async (message, expectedY) => {
      const before = await page.evaluate(()=>viewerR2.state.stream.bytes);
      await send(message);
      await page.waitForFunction(before=>viewerR2.state.stream.bytes>before,before);
      await settled();
      const hit = await pick();
      assert.equal(hit?.position[1] ?? null, expectedY);
    };
    for (const [transport,blocks] of [["tcp","java"],["ws","java"],["tcp","bedrock"],["ws","bedrock"]]) {
      await fetch(fixture+"/configure",{method:"POST",body:JSON.stringify({blocks,delivery:"push"})});
      const old = await page.evaluate(()=>viewerR2.state.world.id);
      const url=transport==="tcp"?(process.env.R2_LINK_TCP || "tcp://127.0.0.1:2426"):(process.env.R2_LINK_WS || "ws://127.0.0.1:2428/r2link");
      await page.evaluate(url=>viewerR2.connectWorld({url}),url);
      await page.waitForFunction(old=>viewerR2.state.world.id>old && viewerR2.state.world.kind==="connection",old);
      await page.evaluate(()=>viewerR2.setCamera([35,70,40],[8,15,8]));
      await settled();
      const empty=await pixels();
      await updated({type:"chunk",mode:"patch",x:0,z:0,sections:[section(0,"stone")]},15);
      const first=await pixels();assert.notEqual(first,empty);
      await updated({type:"chunk",mode:"patch",x:0,z:0,sections:[section(1,"gold_block")]},31);
      assert.notEqual(await pixels(),first);
      await updated({type:"chunk",mode:"replace",x:0,z:0,sections:[section(2,"sea_lantern")]},47);
      await page.waitForSelector('#link-state[data-state="connected"]');
      assert((await page.locator("#link-state").getAttribute("title")).includes(url));
      assert.equal(await page.locator("#link-state").evaluate(el=>getComputedStyle(el).backgroundColor), "rgb(11, 107, 107)");
      await page.screenshot({path:`${out}/${transport}-${blocks}.png`});
      await updated({type:"chunk",mode:"patch",x:0,z:0,deleteSections:[2]},null);
      assert.equal(await pixels(),empty);
      await updated({type:"chunk",mode:"patch",x:0,z:0,sections:[section(0,"stone")]},15);
      await updated({type:"chunk",mode:"delete",x:0,z:0},null);
      assert.equal(await pixels(),empty);
      if (blocks==="bedrock") {
        const water=section(0,"oak_fence");water.layers.push({palette:['minecraft:water ["liquid_depth"=0]'],bits:0});
        await send({type:"chunk",mode:"replace",x:0,z:0,sections:[water]});
        await page.waitForFunction(()=>viewerR2.state.world.linkStats.chunks===1);await settled();
        assert.equal(await page.evaluate(()=>viewerR2.state.world.linkStats.unknownBlocks),0);
      }
      const direction=await page.evaluate(()=>[viewerR2.state.camera.yaw,viewerR2.state.camera.pitch]);
      await send({type:"camera.reset",position:[10,90,30]});
      await page.waitForFunction(()=>viewerR2.state.camera.position[0]===10);
      assert.deepEqual(await page.evaluate(()=>[viewerR2.state.camera.yaw,viewerR2.state.camera.pitch]),direction);
      await send({type:"camera.reset",position:[11,91,31],yaw:0.3,pitch:-0.4});
      await page.waitForFunction(()=>viewerR2.state.camera.yaw===0.3);
      const resetID=await page.evaluate(()=>viewerR2.state.world.id);
      await send({type:"scene.reset",scene:{name:"空场景"}});
      await page.waitForFunction(id=>viewerR2.state.world.id>id,resetID);await settled();
      assert.equal(await page.evaluate(()=>viewerR2.state.world.name),"空场景");
      assert.deepEqual(await page.evaluate(()=>viewerR2.state.camera.position),[11,91,31]);
      assert.equal(await pick(),null);
      await send({type:"chunk",mode:"replace",sections:[section(0,"stone")]});await settled();
      await page.evaluate(()=>viewerR2.setCamera([35,70,40],[8,8,8]));
      await settled();
      const beforeReconnect=await page.evaluate(()=>viewerR2.state.world.id);
      await fetch(fixture+"/disconnect",{method:"POST"});
      await page.waitForFunction(()=>viewerR2.state.world.connection.state==="reconnecting");
      await page.waitForSelector('#link-state[data-state="reconnecting"]');
      assert.equal(await page.locator("#link-state").evaluate(el=>getComputedStyle(el).animationName), "link-pulse");
      assert.match(await page.locator("#loading").textContent(), /每 3 秒重试/);
      if (transport==="tcp" && blocks==="java") await page.screenshot({path:`${out}/reconnecting.png`});
      assert.equal(await page.evaluate(()=>viewerR2.state.world.id),beforeReconnect);
      assert((await pick())!==null,"断线应保留画面");
      await page.waitForFunction(id=>viewerR2.state.world.id>id,beforeReconnect,{timeout:15000});await settled();
      assert.equal(await page.evaluate(()=>viewerR2.state.world.linkStats.chunks),0);
      assert.equal(await pick(),null);
      result.checks.push(`${transport}/${blocks}: patch, replace, section delete, chunk delete, pixels, pick, camera, scene, reconnect`);
    }
    await fetch(fixture+"/configure",{method:"POST",body:JSON.stringify({blocks:"java",delivery:"interest"})});
    const before=await page.evaluate(()=>viewerR2.state.world.id);
    await page.evaluate(()=>viewerR2.connectWorld({url:"tcp://127.0.0.1:2426"}));
    await page.waitForFunction(id=>viewerR2.state.world.id>id,before);
    await page.evaluate(()=>viewerR2.setCamera([8.5,60,8.5],[8.5,0,0]));
    await page.waitForFunction(()=>viewerR2.state.world.linkStats.chunks>0);await settled();
    assert.equal((await pick()).position[1],15);
    const sentBefore=(await (await fetch(fixture+"/stats")).json()).sent;
    await page.evaluate(()=>viewerR2.setCamera([2056.5,60,8.5],[2056.5,0,0]));
    await page.waitForFunction(()=>viewerR2.state.stream.sections>0 && !viewerR2.state.stream.pending);await settled();
    assert.equal((await pick(2056.5,8.5)).position[1],15);
    const stats=await (await fetch(fixture+"/stats")).json();assert(stats.sent>sentBefore);
    assert(stats.ranges.every(r=>r.minX>0));
    await send({type:"chunk",mode:"patch",x:0,z:0,sections:[section(0,"gold_block")]});
    await page.waitForTimeout(300);
    const info=await (await fetch(base+"/api/world")).json();
    const response=await fetch(`${base}/api/world/region?world=${info.id}&x=0&z=0`);
    const body=Buffer.from(await response.arrayBuffer());assert.equal(body.readUInt32LE(4),0,"范围外迟到区块应丢弃");
    await page.evaluate(()=>viewerR2.setCamera([8.5,60,8.5],[8.5,0,0]));
    await page.waitForTimeout(500);await settled();assert.equal((await pick()).position[1],15);
    result.checks.push("interest: camera move, refill, eviction, late outside chunk dropped, return");
    await page.click("#close-world");
    await page.waitForFunction(()=>viewerR2.state.world.name==="Möbius");
    await page.waitForFunction(()=>document.querySelector("#link-state").hidden);
    await page.waitForTimeout(3300);
    assert.equal(await page.evaluate(()=>viewerR2.state.world.connection),undefined);
    assert.deepEqual(result.errors,[]);
  } finally {fs.writeFileSync(out+"/result.json",JSON.stringify(result,null,2));await browser.close();}
  console.log(JSON.stringify(result,null,2));
})().catch(error=>{console.error(error);process.exit(1)});
