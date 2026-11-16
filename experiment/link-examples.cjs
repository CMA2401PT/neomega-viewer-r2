// 两个公开示例的数据必须经过 R2Link，再以真实 Chrome 生成可审阅画面。
const {chromium}=require(process.env.PLAYWRIGHT_MODULE || "/Applications/ChatGPT.app/Contents/Resources/cua_node/lib/node_modules/playwright");
const fs=require("node:fs"),assert=require("node:assert/strict");
const base=process.env.R2_URL || "http://127.0.0.1:2417",out=process.env.R2_OUTPUT || "/tmp/r2link/link";
fs.mkdirSync(out,{recursive:true});
(async()=>{
 const browser=await chromium.launch({executablePath:process.env.CHROME || "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",headless:true,args:["--enable-webgl","--ignore-gpu-blocklist","--use-angle=metal"]});
 const result={errors:[],examples:[]};
 try {
  const page=await browser.newPage({viewport:{width:1280,height:720},deviceScaleFactor:1});
  page.on("pageerror",error=>result.errors.push(String(error)));
  await page.request.post(base+"/api/world/close");await page.goto(base);await page.waitForFunction(()=>globalThis.viewerR2);
  await page.click("#time-flow");
  await page.locator("#fog").evaluate(el=>{el.checked=false;el.dispatchEvent(new Event("change"));});
  for (const [id,value] of [["time",10],["far-radius",16],["still-samples",16]]) {
   await page.locator("#"+id).evaluate((el,value)=>{el.value=String(value);el.dispatchEvent(new Event(el.id==="time"?"input":"change"));},value);
  }
  for (const [name,url] of [["java-world",process.env.R2_JAVA_SOURCE || "ws://127.0.0.1:2424/r2link"],["python",process.env.R2_PYTHON_SOURCE || "tcp://127.0.0.1:2425"]]) {
   const old=await page.evaluate(()=>viewerR2.state.world.id);
   await page.click("#connect-source");await page.locator("#link-url").fill(url);
   await page.screenshot({path:`${out}/${name}-connect.png`});
   await page.click("#link-start");
   await page.waitForFunction(id=>viewerR2.state.world.id>id && viewerR2.state.world.kind==="connection",old);
   if(name==="java-world") await page.evaluate(()=>viewerR2.setCamera([1280,271,1280],[1200,70,1200]));
   await page.waitForFunction(()=>{const s=viewerR2.state.stream;return s.sections>0 && s.loaded===s.wanted && !s.pending && !s.nearPending;},null,{timeout:180000});
   // 大世界会分批送达；保留量稳定后才拍照，避免把首批区域当成全部内容。
   let bytes=-1,stable=0;
   while(stable<3){await page.waitForTimeout(1000);const state=await page.evaluate(()=>viewerR2.state);if(state.world.linkStats.bytes===bytes && !state.stream.pending)stable++;else stable=0;bytes=state.world.linkStats.bytes;}
   assert(await page.evaluate(()=>viewerR2.renderer.pick(viewerR2.state.camera,{width:1280,height:720,x:640,y:360})),"截图中心必须能拾取到推送的方块");
   await page.screenshot({path:`${out}/${name}.png`});
   result.examples.push({name,state:await page.evaluate(()=>viewerR2.state)});
   await page.click("#world-name");await page.screenshot({path:`${out}/${name}-info.png`});await page.keyboard.press("Escape");
  }
  assert.deepEqual(result.errors,[]);
 }finally{fs.writeFileSync(`${out}/examples.json`,JSON.stringify(result,null,2));await browser.close();}
 console.log(JSON.stringify(result.examples.map(row=>({name:row.name,world:row.state.world,stream:row.state.stream})),null,2));
})().catch(error=>{console.error(error);process.exit(1)});
