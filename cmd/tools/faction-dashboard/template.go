package main

import (
	"fmt"
	"html/template"
)

var factionFuncs = template.FuncMap{
	"comma": func(n int) string {
		s := fmt.Sprintf("%d", n)
		out := ""
		for i, c := range s {
			if i > 0 && (len(s)-i)%3 == 0 {
				out += ","
			}
			out += string(c)
		}
		return out
	},
}

var factionTemplate = template.Must(template.New("faction").Funcs(factionFuncs).Parse(`<!DOCTYPE html>
<html lang="en"><head><meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>{{.Faction.Tag}} — Faction Dashboard</title>
<style>
  @import url('https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@400;500;600&display=swap');
  :root{
    --s0:hsl(222,47%,11%);--s1:hsl(222,47%,14%);--s2:hsl(222,45%,18%);--s3:hsl(222,43%,22%);
    --tp:hsl(220,15%,95%);--ts:hsl(220,12%,70%);--tm:hsl(220,10%,50%);
    --green:hsl(150,70%,55%);--blue:hsl(200,70%,55%);--red:hsl(0,70%,60%);
  }
  *{margin:0;padding:0;box-sizing:border-box}
  body{font-family:'JetBrains Mono',monospace;background:var(--s0);color:var(--ts);line-height:1.6;padding:1.5rem}
  .container{max-width:1100px;margin:0 auto}
  .banner{background:linear-gradient(90deg,var(--s2),var(--s0));border-left:4px solid var(--green);
    padding:1rem 1.25rem;border-radius:6px;margin-bottom:1rem}
  .banner h1{color:var(--tp);font-size:1.5rem}
  .banner .meta{color:var(--ts);font-size:.9rem;margin-top:.25rem}
  .banner .meta b{color:var(--green)}
  .tabs{display:flex;gap:4px;flex-wrap:wrap;border-bottom:1px solid var(--s2);margin-bottom:1rem}
  .tab{background:var(--s1);color:var(--ts);border:none;border-radius:6px 6px 0 0;padding:.5rem .9rem;
    font-family:inherit;font-size:.85rem;cursor:pointer}
  .tab.active{background:var(--green);color:var(--s0);font-weight:600}
  .panel{display:none}
  .panel.active{display:block}
  .kpis{display:grid;grid-template-columns:repeat(auto-fit,minmax(160px,1fr));gap:.75rem;margin-bottom:1rem}
  .kpi{background:var(--s1);border:1px solid var(--s2);border-radius:6px;padding:.9rem;text-align:center}
  .kpi .n{color:var(--green);font-size:1.4rem;font-weight:600}
  .kpi small{color:var(--tm);text-transform:uppercase;letter-spacing:.08em;font-size:.7rem}
  .card{background:var(--s1);border:1px solid var(--s2);border-radius:6px;padding:1rem;margin-bottom:1rem}
  .card h3{color:var(--tp);font-size:1rem;margin-bottom:.5rem}
  .lore{white-space:pre-wrap;color:var(--ts)}
  table{width:100%;border-collapse:collapse;font-size:.85rem}
  th,td{text-align:left;padding:.4rem .6rem;border-bottom:1px solid var(--s2)}
  th{color:var(--blue);font-weight:500}
  details{background:var(--s1);border:1px solid var(--s2);border-radius:6px;margin-bottom:.5rem}
  summary{padding:.6rem .9rem;cursor:pointer;color:var(--tp)}
  .empty{color:var(--tm);font-style:italic;padding:.5rem 0}
  .online{color:var(--green)} .offline{color:var(--tm)}
  .footer{margin-top:2rem;padding-top:1rem;border-top:1px solid var(--s2);color:var(--tm);font-size:.8rem}
  a{color:var(--blue)}
</style></head>
<body><div class="container">

<div class="banner">
  <h1>⬢ {{.Faction.Tag}} — {{.Faction.Name}}</h1>
  <div class="meta">💰 <b>{{comma .Faction.Treasury}}</b> &nbsp;·&nbsp; 👥 {{.Faction.MemberCount}} members &nbsp;·&nbsp; 🏠 {{.Faction.OwnedBases}} bases &nbsp;·&nbsp; Leader: {{.Faction.LeaderUsername}}</div>
</div>

<div class="tabs">
  <button class="tab active" data-tab="overview" onclick="showTab(event,'overview')">Overview</button>
  <button class="tab" data-tab="members" onclick="showTab(event,'members')">Members</button>
  <button class="tab" data-tab="diplomacy" onclick="showTab(event,'diplomacy')">Diplomacy</button>
  <button class="tab" data-tab="bases" onclick="showTab(event,'bases')">Bases</button>
  <button class="tab" data-tab="production" onclick="showTab(event,'production')">Production</button>
  <button class="tab" data-tab="storage" onclick="showTab(event,'storage')">Storage</button>
  <button class="tab" data-tab="market" onclick="showTab(event,'market')">Market</button>
  <button class="tab" data-tab="missions" onclick="showTab(event,'missions')">Missions</button>
  <button class="tab" data-tab="rooms" onclick="showTab(event,'rooms')">Rooms</button>
  <button class="tab" data-tab="intel" onclick="showTab(event,'intel')">Intel</button>
</div>

<div class="panel active" data-tab="overview">
  <div class="kpis">
    <div class="kpi"><div class="n">{{comma .Faction.Treasury}}</div><small>Treasury</small></div>
    <div class="kpi"><div class="n">{{.Faction.MemberCount}}</div><small>Members</small></div>
    <div class="kpi"><div class="n">{{.Faction.OwnedBases}}</div><small>Bases</small></div>
    <div class="kpi"><div class="n">{{.Faction.IntelSystems}}</div><small>Intel systems</small></div>
  </div>
  <div class="card"><h3>Charter</h3><div class="lore">{{if .Faction.Charter}}{{.Faction.Charter}}{{else}}<span class="empty">No charter set.</span>{{end}}</div></div>
  <div class="card"><h3>Description</h3><div class="lore">{{if .Faction.Description}}{{.Faction.Description}}{{else}}<span class="empty">No description set.</span>{{end}}</div></div>
  <div class="card"><h3>Identity</h3>
    <table>
      <tr><th>Founded</th><td>{{if .Faction.FoundedUTC}}{{.Faction.FoundedUTC}}{{else}}—{{end}}</td></tr>
      <tr><th>Leader</th><td>{{.Faction.LeaderUsername}}</td></tr>
      <tr><th>Colors</th><td>{{.Faction.PrimaryColor}} / {{.Faction.SecondaryColor}}</td></tr>
      <tr><th>Last collected</th><td>{{.Faction.CapturedAt}}</td></tr>
    </table>
  </div>
</div>

<div class="panel" data-tab="members">
  {{if .Members}}
  <table><tr><th>Member</th><th>Role</th><th>Status</th><th>Joined</th><th>Last seen</th></tr>
  {{range .Members}}<tr>
    <td>{{.Username}}</td><td>{{.Role}}</td>
    <td>{{if .IsOnline}}<span class="online">online</span>{{else}}<span class="offline">offline</span>{{end}}</td>
    <td>{{if .JoinedUTC}}{{.JoinedUTC}}{{else}}—{{end}}</td>
    <td>{{if .LastSeenUTC}}{{.LastSeenUTC}}{{else}}—{{end}}</td>
  </tr>{{end}}</table>
  {{else}}<p class="empty">No members collected.</p>{{end}}
</div>

<div class="panel" data-tab="diplomacy"><p class="empty">Diplomacy — added in Task 11.</p></div>
<div class="panel" data-tab="bases"><p class="empty">Bases — added in Task 11.</p></div>
<div class="panel" data-tab="production"><p class="empty">Production — added in Task 11.</p></div>

<div class="panel" data-tab="storage">
  {{if .Storage}}
  {{range .Storage}}
  <details><summary>{{.BaseID}} — 💰 {{comma .Credits}} · {{.ItemCount}} item types</summary>
    {{if .Items}}<table><tr><th>Item</th><th>Qty</th><th>Size</th></tr>
      {{range .Items}}<tr><td>{{if .Name}}{{.Name}}{{else}}{{.ItemID}}{{end}}</td><td>{{.Quantity}}</td><td>{{.Size}}</td></tr>{{end}}
    </table>{{else}}<p class="empty">Empty.</p>{{end}}
  </details>
  {{end}}
  {{else}}<p class="empty">No storage collected.</p>{{end}}
</div>

<div class="panel" data-tab="market"><p class="empty">Market — added in Task 11.</p></div>
<div class="panel" data-tab="missions"><p class="empty">Missions — added in Task 11.</p></div>
<div class="panel" data-tab="rooms"><p class="empty">Rooms — added in Task 11.</p></div>
<div class="panel" data-tab="intel">
  <div class="card"><h3>Intel coverage</h3>
    <table>
      <tr><th>Systems covered</th><td>{{.Faction.IntelSystems}}</td></tr>
      <tr><th>Trade stations covered</th><td>{{.Faction.IntelTrade}}</td></tr>
    </table>
  </div>
</div>

<div class="footer">Generated by SpaceMolt faction-dashboard · {{.Faction.CapturedAt}}</div>

</div>
<script>
function showTab(e,name){
  document.querySelectorAll('.tab').forEach(function(t){t.classList.toggle('active',t.dataset.tab===name)});
  document.querySelectorAll('.panel').forEach(function(p){p.classList.toggle('active',p.dataset.tab===name)});
}
</script>
</body></html>
`))
