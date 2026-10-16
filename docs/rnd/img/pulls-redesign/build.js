// Builds loaded.html, empty.html and menu.html from mockup.html, then shoots them. Run: node build.js
const fs=require("fs");const tpl=fs.readFileSync(__dirname+"/mockup.html","utf8");
const stg=(a)=>'<div class="stg">'+["worktree","deps","panel","review"].map((n,i)=>'<span class="d '+a[i]+'">'+n+'</span>'+(i<3?'<span class="ln"></span>':"")).join("")+'</div>';
const row=(o)=>`<div class="pr${o.closed?" closed":""}" data-v="${o.v}"><div class="bar"></div>
<div><div class="l1"><span class="repo">${o.repo} <span class="num">#${o.n}</span></span><span class="title">${o.t}</span><span class="au">${o.au}</span></div>
<div class="l2"><span class="vd ${o.v}">${o.vw}</span>${o.sev||""}</div>${o.note?`<div class="note ${o.nc||""}">${o.note}</div>`:""}${o.stg?stg(o.stg):""}</div>
<div class="side"><span class="meta">${o.meta}</span><span class="acts">${o.btn?`<button class="pri ${o.bc||""}">${o.btn}</button><span class="more">&#8943;</span>`:""}</span></div>${o.menu||""}</div>`;
const sv=(h,m,l,t,tc)=>`<span class="sev">${h?`<span class="h">${h} blocker${h>1?"s":""}</span>`:""}${m?`<span class="m">${m} med</span>`:""}${l?`<span>${l} low</span>`:""}<span class="t ${tc}">${t}</span></span>`;
const rows={
wait:row({v:"waiting",vw:"WAITING ON YOU",repo:"openziti/zrok",n:11,t:"Add reconnect backoff to the agent",au:"ekoby",note:'the card asks: "keep the retry cap at 5?"',nc:"q",stg:["x","x","x","r"],meta:"<b>$1.04</b> &middot; 31m",btn:"Answer",bc:"wait"}),
work:row({v:"working",vw:"REVIEWING",repo:"openziti/ziti",n:7402,t:"Router: drop dead links before rerouting",au:"plorenz",note:"panel: 3 of 5 reviewers back",stg:["x","x","r",""],meta:"<b>$0.71</b> so far &middot; 12m",btn:"Open card",bc:"blue"}),
blk:row({v:"blocked",vw:"BLOCKED",repo:"openziti/tlsuv",n:378,t:"Rework the tls handshake",au:"ekoby",sev:sv(2,6,7,"tests 38 ran, 2 failed","bad"),stg:["x","x","x","x"],meta:"<b>$2.40</b> &middot; 1h",btn:"Post review"}),
chg:row({v:"changes",vw:"CHANGES",repo:"openziti/zrok",n:14,t:"Expose a --headless flag on enable",au:"mike-ziti",sev:sv(0,3,5,"tests 12 ran, 12 passed","ok"),stg:["x","x","x","x"],meta:"<b>$1.58</b> &middot; 2h",btn:"Post review"}),
app:row({v:"approve",vw:"APPROVE",repo:"openziti/sdk-golang",n:991,t:"Fix a data race in the xgress reader",au:"ekoby",sev:sv(0,0,3,"tests 41 ran, 41 passed","ok"),stg:["x","x","x","x"],meta:"<b>$1.92</b> &middot; 40m",btn:"Post review"}),
fail:row({v:"failed",vw:"FAILED AT DEPS",repo:"openziti/zrok",n:13,t:"Bump deps",au:"dependabot",note:"npm install exited 1",nc:"e",stg:["x","f","",""],meta:"<b>$0.00</b> &middot; 9m",btn:"Retry",bc:"mute"}),
que:row({v:"queued",vw:"QUEUED",repo:"openziti/edge-api",n:55,t:"Regenerate clients",au:"mike-ziti",stg:["","","",""],meta:"2m",btn:"Start review",bc:"mute"}),
clo:row({v:"approve",closed:true,vw:"POSTED",repo:"openziti/zrok",n:9,t:"Tidy the install docs",au:"ekoby",meta:"closed 3d ago"}),
};
const head=(c)=>`<div class="head"><h1>Pull requests</h1><div class="seg"><span class="on">Open<b>${c[0]}</b></span><span>All<b>${c[1]}</b></span></div><span class="sel">all repos &#9662;</span><span class="sp"></span><button class="btn go">+ Paste a PR</button><span class="btn">&#8943;</span></div>`;
const attn=`<div class="attn"><div class="chipn"><i style="color:var(--wait)">1</i>need you</div><div class="chipn"><i style="color:var(--teal)">3</i>ready to post</div><div class="chipn"><i style="color:var(--blue)">1</i>reviewing</div><div class="chipn"><i style="color:var(--danger)">1</i>failed</div></div>`;
const loaded=head([7,12])+attn+'<div class="group">Needs you <em>1</em></div>'+rows.wait+'<div class="group">Ready to post <em>3</em></div>'+rows.blk+rows.chg+rows.app+'<div class="group">In progress <em>2</em></div>'+rows.work+rows.que+'<div class="group">Failed <em>1</em></div>'+rows.fail;
const menuRow=row({v:"blocked",vw:"BLOCKED",repo:"openziti/tlsuv",n:378,t:"Rework the tls handshake",au:"ekoby",sev:sv(2,6,7,"tests 38 ran, 2 failed","bad"),stg:["x","x","x","x"],meta:"<b>$2.40</b> &middot; 1h",btn:"Post review",menu:'<div class="menu"><div>Show the review card</div><div>Open on GitHub</div><div>Copy PR link</div><div>Move review and card to another room</div><div class="dg">Close card and free its worktree, branch and review</div></div>'});
const empty=head([0,12])+`<div class="empty"><div class="ico">&#10003;</div><h2>Nothing under review</h2><p>Paste a pull request link and atrium reviews it: a panel reads the diff, runs the tests and gives you a verdict to post.</p><button class="btn go">+ Paste a PR</button></div><div class="group">Closed recently <em>1</em></div>`+rows.clo;
const loading=head(["&hellip;","&hellip;"])+'<div class="sk"></div><div class="sk"></div><div class="sk"></div>';
const pages={loaded,empty,menu:head([7,12])+menuRow+"<div style='height:120px'></div>",loading};
for(const k in pages)fs.writeFileSync(__dirname+"/"+k+".html",tpl.replace("<!--SCENE-->",pages[k]));
(async()=>{const {chromium}=require("playwright");const b=await chromium.launch();
for(const k in pages){for(const [n,w,h] of [["desktop",1280,900],["phone",390,844]]){
const p=await b.newPage({viewport:{width:w,height:h},deviceScaleFactor:n=="phone"?2:1});await p.goto("file://"+__dirname+"/"+k+".html");
await p.screenshot({path:__dirname+"/"+k+"-"+n+".png",fullPage:k!="menu"&&k!="loading"?true:false});await p.close();}}
await b.close();})();
