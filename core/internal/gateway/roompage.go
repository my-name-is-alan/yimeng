package gateway

import (
	"html/template"
	"net/http"
)

var roomPageTmpl = template.Must(template.New("room").Parse(`<!doctype html><html lang="zh-CN"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><title>一起听 · {{.}}</title>
<style>body{margin:0;font:15px system-ui;background:#0f1115;color:#e8e8ea}main{max-width:720px;margin:0 auto;padding:16px}
#stage{position:relative;height:220px;overflow:hidden;border-radius:12px;background:#1a1d24;margin:12px 0}
.dm{position:absolute;white-space:nowrap;animation:fly 8s linear forwards;font-weight:600;text-shadow:0 1px 2px #000}
@keyframes fly{from{left:100%}to{left:-60%}}form{display:flex;gap:8px}input{flex:1;padding:10px;border-radius:8px;border:1px solid #333;background:#16181d;color:inherit}
button{padding:10px 16px;border:0;border-radius:8px;background:#7c5cff;color:#fff}#log{font-size:13px;opacity:.75;max-height:200px;overflow:auto}</style></head>
<body><main><h2 id="title">一起听 · {{.}}</h2><div id="now">等待房主播放…</div><div id="stage"></div>
<form id="f"><input id="t" maxlength="100" placeholder="发送弹幕" autocomplete="off"><button>发送</button></form><div id="log"></div></main>
<script>
const code={{.}};const tok=localStorage.getItem("yimeng.token")||"";const H={"Authorization":"Bearer "+tok,"Content-Type":"application/json"};
const $=s=>document.querySelector(s);let lane=0;
function say(s){const d=document.createElement("div");d.textContent=s;$("#log").prepend(d)}
function fly(m){const d=document.createElement("div");d.className="dm";d.textContent=m.name+"："+m.text;d.style.top=(10+(lane++%6)*34)+"px";if(m.color)d.style.color=m.color;$("#stage").append(d);setTimeout(()=>d.remove(),8200)}
function st(s){$("#now").textContent=s.track_id?((s.playing?"▶ ":"⏸ ")+s.track_id+" · "+Math.round(s.position_ms/1000)+"s"):"等待房主播放…"}
async function start(){if(!tok){say("请先在管理页面登录后再打开本页");return}
 const r=await fetch("/api/v1/rooms/"+encodeURIComponent(code)+"/join",{method:"POST",headers:H});if(!r.ok){say("无法加入房间："+r.status);return}
 const room=await r.json();$("#title").textContent="一起听 · "+room.name;st(room.state);
 const es=new EventSource("/api/v1/rooms/"+room.id+"/events?access_token="+encodeURIComponent(tok));
 es.addEventListener("room.state",e=>st(JSON.parse(e.data).state));es.addEventListener("room.danmaku",e=>fly(JSON.parse(e.data)));
 es.addEventListener("room.member_joined",e=>say(JSON.parse(e.data).name+" 加入了"));es.addEventListener("room.member_left",()=>say("有人离开了"));
 $("#f").onsubmit=async ev=>{ev.preventDefault();const t=$("#t").value.trim();if(!t)return;$("#t").value="";
  const x=await fetch("/api/v1/rooms/"+room.id+"/danmaku",{method:"POST",headers:H,body:JSON.stringify({text:t})});if(!x.ok)say("发送失败："+x.status)}}
start();
</script></body></html>`))

// GET /room/{code} — public web page for a 一起听 room (data via the API with the web token).
func (d Deps) roomPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	roomPageTmpl.Execute(w, r.PathValue("code"))
}
