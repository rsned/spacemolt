#!/usr/bin/env python3
"""Import-struct audit: compare Go decode structs against the live API.

Three sources, flattened to dotted JSON paths and diffed:
  1. Go     - structs in pkg/game/serverapi/ (and cmd/data/import-* importers),
              dumped by ./cmd/data/struct-audit (go/ast) and fully expanded.
  2. Spec   - server_docs/openapi.json component schemas (success oneOf variant
              chosen by best overlap).
  3. Live   - data/game-api/latest/*.json snapshots (ground truth; subkeys of Go
              map[...] fields are treated as covered).

Run from the spacemolt repo root:
    python3 cmd/data/struct-audit/audit.py
Writes server_docs/import_struct_audit.md (gitignored).

Re-run after each data scrape: the openapi symlink + latest/ snapshots update,
and the report reflects current drift. Confidence is graded by whether a live
snapshot confirms a finding (parent array/object must be populated to confirm a
"stale" field).
"""
import json, os, subprocess, sys

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", ".."))
LATEST = f"{ROOT}/data/game-api/latest"
OPENAPI = f"{ROOT}/server_docs/openapi.json"
OUT = f"{ROOT}/server_docs/import_struct_audit.md"
AUDIT_CMD = os.path.dirname(os.path.abspath(__file__))

def dump_structs(*dirs):
    res = subprocess.run(["go", "run", AUDIT_CMD, *dirs], cwd=ROOT,
                         capture_output=True, text=True)
    if res.returncode != 0:
        sys.exit(f"struct dump failed for {dirs}:\n{res.stderr}")
    return json.loads(res.stdout)

oa = json.load(open(OPENAPI))
schemas = oa["components"]["schemas"]
gomap = {s["name"]: s["fields"] for s in dump_structs("pkg/game/serverapi")}

PRIM = {"string":"string","bool":"bool","int":"number","int8":"number","int16":"number",
 "int32":"number","int64":"number","uint":"number","uint8":"number","uint16":"number",
 "uint32":"number","uint64":"number","float32":"number","float64":"number",
 "json.RawMessage":"raw","interface{}":"any","time.Time":"string","json.Number":"number","byte":"number"}

def gnorm(t):
    t = t.strip()
    while t.startswith("*"): t = t[1:]
    return t
def gkind(t, m):
    t = gnorm(t)
    if t.startswith("[]"): return "array", t[2:]
    if t.startswith("map["): return "map", t
    if t in PRIM: return "scalar", PRIM[t]
    if t in m: return "struct", t
    return "scalar", "unknown"
def flat_go(typename, m, prefix="", seen=None, out=None, d=0):
    if out is None: out = {}
    if seen is None: seen = set()
    if d > 12 or typename not in m or typename in seen: return out
    seen = seen | {typename}
    for f in m[typename]:
        if f.get("inline"):
            et = gnorm(f["go_type"])
            if et in m: flat_go(et, m, prefix, seen, out, d+1)
            continue
        jn = f["json"]
        if jn in ("", "-"): continue
        path = prefix + jn
        k, res = gkind(f["go_type"], m)
        if k == "array":
            ak, ares = gkind(res, m); ap = path+"[]"
            if ak == "struct":
                out[ap] = "object"; flat_go(ares, m, ap+".", seen, out, d+1)
            else: out[ap] = ares if ak == "scalar" else ak
            out.setdefault(path, "array")
        elif k == "struct":
            out[path] = "object"; flat_go(res, m, path+".", seen, out, d+1)
        elif k == "map": out[path] = "map"
        else: out[path] = res
    return out

def drop_map_covered(paths, gopaths):
    mp = [p+"." for p, k in gopaths.items() if k == "map"]
    return [p for p in paths if not any(p.startswith(x) for x in mp)]
def ancestors(p):
    segs = p.split('.'); res = []
    for i in range(1, len(segs)):
        a = '.'.join(segs[:i]); res.append(a)
        if a.endswith('[]'): res.append(a[:-2])
    return res
def parent(p):
    if '.' in p: return p.rsplit('.', 1)[0]
    return p[:-2] if p.endswith('[]') else ''
def collapse(paths):
    s = set(paths)
    return [p for p in paths if not any(a in s for a in ancestors(p))]

OAT = {"string":"string","integer":"number","number":"number","boolean":"bool","object":"object","array":"array"}
def oatype(n):
    t = n.get("type")
    if isinstance(t, list):
        t = [x for x in t if x != "null"]; t = t[0] if t else None
    return t
def flat_oa(node, prefix="", out=None, d=0):
    if out is None: out = {}
    if d > 12 or not isinstance(node, dict): return out
    if "oneOf" in node or "anyOf" in node:
        for v in (node.get("oneOf") or node.get("anyOf")): flat_oa(v, prefix, out, d+1)
        return out
    if "allOf" in node:
        for v in node["allOf"]: flat_oa(v, prefix, out, d+1)
        return out
    props = node.get("properties")
    if props:
        for k, v in props.items():
            if not isinstance(v, dict): continue
            path = prefix+k; vt = oatype(v)
            if vt == "array":
                items = v.get("items", {}); it = oatype(items); ap = path+"[]"
                out[path] = "array"
                if isinstance(items, dict) and (items.get("properties") or "oneOf" in items or "allOf" in items):
                    out[ap] = "object"; flat_oa(items, ap+".", out, d+1)
                else: out[ap] = OAT.get(it, it or "any")
            elif vt == "object" or v.get("properties"):
                out[path] = "object"; flat_oa(v, path+".", out, d+1)
            else: out[path] = OAT.get(vt, vt or "any")
    return out
def variants(s): return s.get("oneOf") or s.get("anyOf") or [s]
def best_oa(s, gp):
    best, bs = {}, -1
    for v in variants(s):
        fp = flat_oa(v); sc = len(set(fp) & gp)
        if sc > bs: bs, best = sc, fp
    return best

def jtype(v):
    if isinstance(v, bool): return "bool"
    if isinstance(v, (int, float)): return "number"
    if isinstance(v, str): return "string"
    if v is None: return "null"
    return "any"
def flat_json(obj, prefix="", out=None):
    if out is None: out = {}
    if isinstance(obj, dict):
        for k, v in obj.items():
            p = prefix+k
            if isinstance(v, dict): out[p] = "object"; flat_json(v, p+".", out)
            elif isinstance(v, list):
                out[p] = "array"
                for e in v:
                    if isinstance(e, dict): out.setdefault(p+"[]", "object"); flat_json(e, p+"[].", out)
                    elif not isinstance(e, list): out.setdefault(p+"[]", jtype(e))
            else: out[p] = jtype(v)
    return out
def load_snap(name):
    p = f"{LATEST}/{name}.json"
    if not os.path.exists(p): return None
    try: return flat_json(json.load(open(p)))
    except Exception: return None

SNAP_SERVERAPI = {
 "get_status":"GetStatusResponse","get_system":"GetSystemResponse","get_poi":"GetPOIResponse",
 "get_ship":"GetShipResponse","get_cargo":"GetCargoResponse","get_map":"GetMapResponse",
 "get_nearby":"GetNearbyResponse","get_missions":"GetMissionsResponse",
 "get_active_missions":"GetActiveMissionsResponse","get_wrecks":"GetWrecksResponse",
 "get_base":"GetBaseResponse","get_version":"GetVersionResponse","get_notes":"GetNotesResponse",
 "get_commands":"GetCommandsResponse","view_market":"ViewMarketResponse",
 "view_orders":"ViewOrdersResponse","view_storage":"ViewStorageResponse",
 "browse_ships":"BrowseShipsResponse","captains_log_list":"CaptainsLogListResponse",
 "catalog_items":"CatalogResponse","catalog_ships":"CatalogResponse",
 "catalog_recipes":"CatalogResponse","catalog_skills":"CatalogResponse",
}

serv = {}
for name in gomap:
    if name not in schemas: continue
    god = flat_go(name, gomap); gp = set(god)
    op = set(best_oa(schemas[name], gp))
    sf = [f for f, s in SNAP_SERVERAPI.items() if s == name]
    snap = load_snap(sf[0]) if sf else None
    sp = set(snap) if snap else None
    def dmc(paths): return set(drop_map_covered(sorted(paths), god))
    rec = {"n_go":len(gp),"n_oa":len(op),"has_snap":sp is not None}
    if sp is not None:
        rec["live_missing"] = collapse(sorted(dmc(sp-gp)))
        rec["spec_missing_only"] = sorted(dmc((op-gp)-sp))
        cstale = [p for p in sorted((gp-op)-sp) if p != "action"]
        rec["action_extra"] = ["action"] if "action" in (gp-op)-sp else []
        cstale = collapse(cstale)
        rec["confirmed_stale"] = [p for p in cstale if parent(p) == '' or parent(p) in sp]
        rec["unconfirmed_stale"] = [p for p in cstale if not(parent(p) == '' or parent(p) in sp)]
    else:
        miss = sorted(dmc(op-gp)); st = sorted(gp-op)
        rec["missing"] = miss
        rec["action_extra"] = [p for p in st if p == "action"]
        rec["stale"] = [p for p in st if p != "action"]
    serv[name] = rec

IMPMAP = [
 ("import-catalog-items","ItemsResponse","catalog_items"),
 ("import-catalog-ships","ShipsResponse","catalog_ships"),
 ("import-catalog-recipes","RecipesResponse","catalog_recipes"),
 ("import-catalog-skills","SkillsResponse","catalog_skills"),
 ("import-base-data","BaseResponse","get_base"),
 ("import-map-data","MapResponse","get_map"),
]
PAGINATION = {"message","page","page_size","total","total_pages","type","total_count"}
imp = {}
for tool, root, snapf in IMPMAP:
    m = {s["name"]: s["fields"] for s in dump_structs(f"cmd/data/{tool}")}
    if root not in m: continue
    god = flat_go(root, m); gp = set(god)
    snap = load_snap(snapf)
    if snap is None: continue
    sp = set(snap)
    lm = drop_map_covered(sorted(sp-gp), god)
    imp[tool] = {"root":root,"snap":snapf,"n_go":len(gp),"n_snap":len(sp),
        "live_missing":[p for p in lm if p not in PAGINATION],
        "pagination_missing":sorted(set(lm) & PAGINATION),
        "unused":sorted(gp-sp)}

# ----- markdown -----
def fmtlist(lst, n=200):
    return "".join(f"  - `{p}`\n" for p in lst[:n]) + ("" if len(lst) <= n else f"  - …(+{len(lst)-n} more)\n")
L = []
L.append("# Import Struct Audit — serverapi vs live API\n")
L.append(f"Generated against `server_docs/openapi.json` (HTTP API, gameserver `{oa['info'].get('x-gameserver-version','?')}`) and live REST snapshots in `data/game-api/latest/`.\n")
L.append("\n## Method\n")
L.append("Go structs (`pkg/game/serverapi/` + `cmd/data/import-*`), openapi schemas, and live snapshots are flattened to dotted JSON paths and diffed. Go `map[...]` subkeys are treated as covered. A 'stale' field is only *confirmed* when its parent array/object is populated in the live snapshot.\n")
L.append("\n### Caveats\n")
L.append("- Snapshots/openapi describe the **HTTP REST** API. Go also decodes the **WebSocket** protocol (bundles `action`/`nearby`/`poi`/`current_tick`) — reported separately, not as deletions.\n")
L.append("- omitempty conditional fields (transit/combat state) can appear 'stale' if the sample didn't exercise that state (see D2).\n")
L.append("\n## A. Fields in LIVE data that Go structs LACK (add these)\n")
for n, r in sorted([(n, r) for n, r in serv.items() if r.get("live_missing")], key=lambda x:-len(x[1]['live_missing'])):
    L.append(f"\n### {n} — {len(r['live_missing'])} missing\n"); L.append(fmtlist(r['live_missing']))
L.append("\n## B. Spec-declared fields Go lacks (no live sample — lower confidence)\n")
for n, r in sorted([(n, r) for n, r in serv.items() if r.get("spec_missing_only")], key=lambda x:-len(x[1]['spec_missing_only'])):
    L.append(f"\n### {n} — {len(r['spec_missing_only'])}\n"); L.append(fmtlist(r['spec_missing_only']))
L.append("\n## C. Structs without a snapshot — Go vs spec only (2-way)\n")
for n, r in sorted([(n, r) for n, r in serv.items() if not r['has_snap'] and (r.get('missing') or r.get('stale'))], key=lambda x:-(len(x[1].get('missing',[]))+len(x[1].get('stale',[])))):
    miss, st = r.get('missing', []), r.get('stale', [])
    L.append(f"\n### {n} — {len(miss)} missing / {len(st)} stale\n")
    if miss: L.append("**Missing:**\n"+fmtlist(miss, 60))
    if st: L.append("**Stale:**\n"+fmtlist(st, 60))
L.append("\n## D. Go fields absent from both spec and live (candidate removals) — confirmed\n")
for n, r in sorted([(n, r) for n, r in serv.items() if r.get("confirmed_stale")], key=lambda x:-len(x[1]['confirmed_stale'])):
    L.append(f"\n### {n} — {len(r['confirmed_stale'])}\n"); L.append(fmtlist(r['confirmed_stale']))
L.append("\n## D2. Stale candidates (UNCONFIRMED — empty/absent parent in snapshot)\n")
for n, r in sorted([(n, r) for n, r in serv.items() if r.get("unconfirmed_stale")], key=lambda x:-len(x[1]['unconfirmed_stale'])):
    L.append(f"\n### {n} — {len(r['unconfirmed_stale'])}\n"); L.append(fmtlist(r['unconfirmed_stale'], 40))
L.append("\n## E. `action` present in Go but not spec/live (protocol echo — usually harmless)\n")
L.append(", ".join(f"`{n}`" for n in sorted(n for n, r in serv.items() if r.get("action_extra")))+"\n")
L.append("\n## F. KB import tools (`cmd/data/import-*`) vs live snapshot\n")
for t, r in imp.items():
    L.append(f"\n### {t} — `{r['root']}` vs `{r['snap']}.json`\n")
    if r['live_missing']: L.append(f"**Live fields not decoded ({len(r['live_missing'])}):**\n"+fmtlist(r['live_missing'], 80))
    if r['pagination_missing']: L.append(f"**Pagination metadata ignored:** {', '.join('`'+p+'`' for p in r['pagination_missing'])}\n")
    if r['unused']: L.append(f"**Struct fields not in snapshot (verify):** {', '.join('`'+p+'`' for p in r['unused'])}\n")
open(OUT, "w").write("".join(L))
print(f"matched {len(serv)} serverapi structs ({sum(1 for r in serv.values() if r['has_snap'])} with snapshot), {len(imp)} import tools")
print(f"wrote {OUT}")
