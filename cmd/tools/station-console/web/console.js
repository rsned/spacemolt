// Conjunction Junction station console: a rotating hologram of the station
// whose zones take the colour of the systems they stand for, with labels,
// detail popups and 48 h trends. All alert thresholds live in Go
// (pkg/stationstate); this page only renders /api/state.
//
// Zone maps (/zones/{model}.json) place zones on a mesh. They work in
// NORMALISED MODEL SPACE: the mesh is turned so its up axis (orient.upAxis:
// "thinnest" | "longest" | "x" | "y" | "z") is +Y and centred on its bounding
// box; x and z are divided by the largest distance of any vertex from the
// axle (so r = 1 is the outer rim) and y by half the model's height (so the
// bottom is -1 and the top +1). orient.tiltX (radians) tips the model about
// the screen's horizontal axis for display only (negative tips its top towards
// the viewer, showing more of the upper face); it does not move zones.
//
// A region matches a vertex when every constraint it carries holds:
//   r: [lo, hi)  distance from the axle, hypot(x, z)
//   a: [lo, hi)  fraction of a turn around the axle, (atan2(z, x) + PI) / 2PI;
//                lo > hi wraps through 0
//   h: [lo, hi)  height y
//   min/max: [x, y, z] corners of a box
// "shape" (radial | sector | band | box) names the intent. The first matching
// region wins; unmatched vertices are hull (grey-cyan, damage tint only).
// labels[zone].offset is the label's pixel offset from screen centre (at a
// 900 px wide stage); labels[zone].anchor ([x, y, z], normalised) pins where
// its leader line meets the model, otherwise the zone's centroid is used.

// ---- pure helpers -------------------------------------------------------

const SEVERITY_COLORS = { ok: '#3fb8ff', strained: '#ffb84d', critical: '#ff3b3b', nodata: '#5a7080' };

/** severityColor is the CSS hex colour for a severity name from /api/state. */
export function severityColor(sev) {
  return SEVERITY_COLORS[sev] ?? SEVERITY_COLORS.nodata;
}

const inRange = (v, [lo, hi]) => (lo <= hi ? v >= lo && v < hi : v >= lo || v < hi);

/** zoneForVertex returns the zone owning a normalised vertex, or null (hull). */
export function zoneForVertex(map, x, y, z) {
  const r = Math.hypot(x, z);
  const a = (Math.atan2(z, x) + Math.PI) / (2 * Math.PI);
  for (const g of map.regions ?? []) {
    if (g.r && !inRange(r, g.r)) continue;
    if (g.a && !inRange(a, g.a)) continue;
    if (g.h && !inRange(y, g.h)) continue;
    if (g.min && (x < g.min[0] || y < g.min[1] || z < g.min[2])) continue;
    if (g.max && (x > g.max[0] || y > g.max[1] || z > g.max[2])) continue;
    return g.zone;
  }
  return null;
}

/** shieldLevel maps shield % onto 0 (cyan) .. 1 (yellow) .. 2 (red). */
export function shieldLevel(pct) {
  if (pct >= 60) return 0;
  if (pct >= 25) return (60 - pct) / 35;
  return 1 + (25 - pct) / 25;
}

/** ago renders a duration in minutes as "N min" or "Nh Mm". */
export function ago(minutes) {
  const m = Math.max(0, Math.round(minutes));
  return m < 120 ? `${m} min` : `${Math.floor(m / 60)}h ${m % 60}m`;
}

const esc = s => String(s ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]);
const num = n => (n == null ? '—' : Math.round(n).toLocaleString('en-US'));
const pctOf = (a, b) => (b > 0 ? (a / b) * 100 : 0);

// ---- three.js scene -------------------------------------------------------

import * as THREE from 'three';
import { GLTFLoader } from 'three/addons/loaders/GLTFLoader.js';
import { OrbitControls } from 'three/addons/controls/OrbitControls.js';

const ZONES = ['core', 'life', 'fuel', 'production', 'tourism', 'shipyard', 'defense', 'commerce'];
const HULL = 255; // zone index of unmatched vertices
const HULL_COLOR = new THREE.Color(0x6a9db0);
const DEFAULT_OFFSETS = {
  core: [0, -300], life: [-290, -130], fuel: [-300, 0], shipyard: [-280, 130],
  production: [280, -180], defense: [300, -60], tourism: [300, 60], commerce: [260, 180],
};
const FOLLOW = 0.2; // how far a label drifts with its anchor as the model turns
// Generic map for models without their own: the wheel's rings and sectors.
const FALLBACK_MAP = {
  orient: { upAxis: 'thinnest', tiltX: -0.35 },
  regions: [
    { zone: 'core', shape: 'radial', r: [0, 0.2] },
    { zone: 'life', shape: 'radial', r: [0.2, 0.4] },
    { zone: 'fuel', shape: 'radial', r: [0.4, 0.72] },
    { zone: 'production', shape: 'sector', r: [0.72, 9], a: [0, 0.2] },
    { zone: 'tourism', shape: 'sector', r: [0.72, 9], a: [0.2, 0.4] },
    { zone: 'shipyard', shape: 'sector', r: [0.72, 9], a: [0.4, 0.6] },
    { zone: 'defense', shape: 'sector', r: [0.72, 9], a: [0.6, 0.8] },
    { zone: 'commerce', shape: 'sector', r: [0.72, 9], a: [0.8, 1.0] },
  ],
  labels: {},
};
const MODEL_KEY = 'cj-console-model';
const STATE_EVERY = 60_000;
const HISTORY_EVERY = 600_000;
const TREND_HOURS = 48;

const $ = id => document.getElementById(id);
const stage = $('stage');
const renderer = new THREE.WebGLRenderer({ antialias: true, alpha: true });
renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));
renderer.setSize(stage.clientWidth, stage.clientHeight);
stage.prepend(renderer.domElement);
const scene = new THREE.Scene();
const camera = new THREE.PerspectiveCamera(35, stage.clientWidth / stage.clientHeight, 0.01, 100);
camera.position.set(0, 1.4, 3.4);
const controls = new OrbitControls(camera, renderer.domElement);
controls.enableDamping = true;
controls.autoRotate = true;
controls.autoRotateSpeed = 0.6;
controls.minDistance = 1.2;
controls.maxDistance = 9;

// The loaded model: a tilted group holding the faint solid and the edge lines.
let model = null; // {id, group, lines, zoneIdx: Uint8Array, noise: Float32Array, anchors: {zone: Vector3}, map, radius}
let shells = [];
let state = null;
let history = [];
let focus = null;
let openZone = null;
let loadSeq = 0;

function showMsg(text) {
  $('msg').textContent = text;
}

function upAxisIndex(size, upAxis) {
  const dims = [size.x, size.y, size.z];
  switch (upAxis) {
    case 'x': return 0;
    case 'y': return 1;
    case 'z': return 2;
    case 'longest': return dims.indexOf(Math.max(...dims));
    default: return dims.indexOf(Math.min(...dims));
  }
}

async function fetchZoneMap(id) {
  try {
    const res = await fetch(`/zones/${encodeURIComponent(id)}.json`);
    if (res.ok) return await res.json();
  } catch (err) {
    console.warn('zone map', id, err);
  }
  return FALLBACK_MAP;
}

function loadGLB(url) {
  return new Promise((resolve, reject) => new GLTFLoader().load(url, resolve, undefined, reject));
}

function disposeModel() {
  if (!model) return;
  scene.remove(model.group);
  model.group.traverse(o => {
    o.geometry?.dispose();
    o.material?.dispose();
  });
  model = null;
}

async function loadModel(id) {
  const seq = ++loadSeq;
  showMsg('LOADING MODEL');
  $('tags').querySelectorAll('.ztag').forEach(t => (t.style.display = 'none'));
  $('leads').replaceChildren();
  let gltf, map;
  try {
    [gltf, map] = await Promise.all([loadGLB(`/models/${encodeURIComponent(id)}/mesh.glb`), fetchZoneMap(id)]);
  } catch (err) {
    if (seq !== loadSeq) return;
    disposeModel();
    showMsg('MODEL UNAVAILABLE');
    console.error('mesh', id, err);
    return;
  }
  if (seq !== loadSeq) return; // a newer choice superseded this one
  disposeModel();
  model = buildModel(id, gltf, map);
  scene.add(model.group);
  rebuildShells();
  buildTags();
  showMsg('');
  console.info(`station-console: loaded ${id}, ${model.zoneIdx.length} edge vertices`);
}

function buildModel(id, gltf, map) {
  const root = gltf.scene;
  root.updateMatrixWorld(true);
  const geos = [];
  root.traverse(o => {
    if (!o.isMesh) return;
    const g = new THREE.BufferGeometry();
    g.setAttribute('position', o.geometry.attributes.position.clone());
    if (o.geometry.index) g.setIndex(o.geometry.index.clone());
    g.applyMatrix4(o.matrixWorld);
    geos.push(g);
  });
  const box = new THREE.Box3();
  for (const g of geos) {
    g.computeBoundingBox();
    box.union(g.boundingBox);
  }
  const up = upAxisIndex(box.getSize(new THREE.Vector3()), map.orient?.upAxis);
  const turn = new THREE.Matrix4();
  if (up === 0) turn.makeRotationZ(Math.PI / 2);
  if (up === 2) turn.makeRotationX(-Math.PI / 2);
  box.makeEmpty();
  for (const g of geos) {
    g.applyMatrix4(turn);
    g.computeBoundingBox();
    box.union(g.boundingBox);
  }
  const centre = box.getCenter(new THREE.Vector3());
  const size = box.getSize(new THREE.Vector3());
  const k = 1.9 / Math.max(size.x, size.y, size.z);
  for (const g of geos) {
    g.translate(-centre.x, -centre.y, -centre.z);
    g.scale(k, k, k);
  }

  // One LineSegments over every mesh's feature edges.
  const parts = geos.map(g => new THREE.EdgesGeometry(g, 20));
  const total = parts.reduce((n, e) => n + e.attributes.position.array.length, 0);
  const pos = new Float32Array(total);
  let off = 0;
  for (const e of parts) {
    pos.set(e.attributes.position.array, off);
    off += e.attributes.position.array.length;
    e.dispose();
  }
  const n = pos.length / 3;
  let rMax = 1e-6;
  let yMax = 1e-6;
  for (let i = 0; i < n; i++) {
    rMax = Math.max(rMax, Math.hypot(pos[i * 3], pos[i * 3 + 2]));
    yMax = Math.max(yMax, Math.abs(pos[i * 3 + 1]));
  }
  const zoneIdx = new Uint8Array(n);
  const noise = new Float32Array(n);
  const sums = ZONES.map(() => [0, 0, 0, 0]);
  for (let i = 0; i < n; i++) {
    const x = pos[i * 3], y = pos[i * 3 + 1], z = pos[i * 3 + 2];
    const zi = ZONES.indexOf(zoneForVertex(map, x / rMax, y / yMax, z / rMax));
    zoneIdx[i] = zi < 0 ? HULL : zi;
    noise[i] = Math.random();
    if (zi >= 0) {
      const s = sums[zi];
      s[0] += x; s[1] += y; s[2] += z; s[3]++;
    }
  }
  const anchors = {};
  ZONES.forEach((zone, zi) => {
    const pinned = map.labels?.[zone]?.anchor;
    if (pinned) {
      anchors[zone] = new THREE.Vector3(pinned[0] * rMax, pinned[1] * yMax, pinned[2] * rMax);
      return;
    }
    const s = sums[zi];
    if (!s[3]) {
      anchors[zone] = new THREE.Vector3();
      return;
    }
    // A ring's centroid sits on the axle, off the zone: snap to its nearest vertex.
    const c = new THREE.Vector3(s[0] / s[3], s[1] / s[3], s[2] / s[3]);
    let best = Infinity, bi = -1;
    for (let i = 0; i < n; i++) {
      if (zoneIdx[i] !== zi) continue;
      const d = (pos[i * 3] - c.x) ** 2 + (pos[i * 3 + 1] - c.y) ** 2 + (pos[i * 3 + 2] - c.z) ** 2;
      if (d < best) { best = d; bi = i; }
    }
    anchors[zone] = Math.sqrt(best) > 0.12 * rMax ? new THREE.Vector3(pos[bi * 3], pos[bi * 3 + 1], pos[bi * 3 + 2]) : c;
  });

  const eg = new THREE.BufferGeometry();
  eg.setAttribute('position', new THREE.BufferAttribute(pos, 3));
  eg.setAttribute('color', new THREE.BufferAttribute(new Float32Array(n * 3), 3));
  eg.computeBoundingSphere();
  const lines = new THREE.LineSegments(eg, new THREE.LineBasicMaterial({
    vertexColors: true, transparent: true, opacity: 0.42, blending: THREE.AdditiveBlending, depthWrite: false,
  }));
  const group = new THREE.Group();
  group.rotation.x = -(map.orient?.tiltX ?? 0);
  const solidMat = new THREE.MeshBasicMaterial({
    color: 0x3fb8ff, transparent: true, opacity: 0.07, blending: THREE.AdditiveBlending, depthWrite: false,
  });
  for (const g of geos) group.add(new THREE.Mesh(g, solidMat));
  group.add(lines);
  return { id, group, lines, solidMat, zoneIdx, noise, anchors, map, radius: eg.boundingSphere.radius };
}

// One shell for the built-in shield plus one per shield projector; all share
// the aggregate shield colour, and outer shells drop out first as it falls.
function rebuildShells() {
  const want = state?.ShieldShells ?? 1;
  if (shells.length === want && shells.radius === model?.radius) return;
  for (const s of shells) {
    scene.remove(s);
    s.geometry.dispose();
    s.material.dispose();
  }
  shells = [];
  const r0 = (model?.radius ?? 1) * 1.1;
  for (let i = 0; i < want; i++) {
    const s = new THREE.Mesh(new THREE.IcosahedronGeometry(r0 + 0.08 * i, 4), new THREE.MeshBasicMaterial({
      color: 0x3fb8ff, wireframe: true, transparent: true, blending: THREE.AdditiveBlending, depthWrite: false,
    }));
    s.userData.op = 0.2 * 0.6 ** i;
    scene.add(s);
    shells.push(s);
  }
  shells.radius = model?.radius;
}

const SEV_THREE = Object.fromEntries(Object.keys(SEVERITY_COLORS).map(k => [k, new THREE.Color(severityColor(k))]));

function levelColor(level, out = new THREE.Color()) {
  return level <= 1
    ? out.copy(SEV_THREE.ok).lerp(SEV_THREE.strained, Math.max(0, level))
    : out.copy(SEV_THREE.strained).lerp(SEV_THREE.critical, Math.min(1, level - 1));
}

function zoneSeverity(zone) {
  return state?.Zones?.find(z => z.Zone === zone)?.Severity ?? 'nodata';
}

// ---- labels ---------------------------------------------------------------

const tagEl = {};
const leadEl = {};

function buildTags() {
  const host = $('tags');
  for (const zone of ZONES) {
    if (!tagEl[zone]) {
      const d = document.createElement('div');
      d.className = 'ztag';
      d.innerHTML = '<span></span><b></b>';
      d.onmouseenter = () => (focus = zone);
      d.onmouseleave = () => (focus = null);
      d.onclick = () => openPopup(zone);
      host.appendChild(d);
      tagEl[zone] = d;
    }
    tagEl[zone].style.display = '';
    const ln = document.createElementNS('http://www.w3.org/2000/svg', 'polyline');
    ln.setAttribute('fill', 'none');
    $('leads').appendChild(ln);
    leadEl[zone] = ln;
  }
  renderTagText();
}

function renderTagText() {
  for (const zone of ZONES) {
    const el = tagEl[zone];
    if (!el) continue;
    const zs = state?.Zones?.find(z => z.Zone === zone);
    el.firstChild.textContent = zs?.Label ?? zone.toUpperCase();
    el.lastChild.textContent = zs?.Brief ?? 'no data';
    const sev = zs?.Severity ?? 'nodata';
    el.style.color = severityColor(sev);
    el.classList.toggle('alert', sev === 'strained' || sev === 'critical');
  }
}

const tmp = new THREE.Vector3();

function placeTags() {
  const w = renderer.domElement.clientWidth, h = renderer.domElement.clientHeight;
  for (const zone of ZONES) {
    const el = tagEl[zone], ln = leadEl[zone], a = model.anchors[zone];
    if (!el || !ln || !a) continue;
    tmp.copy(a).applyMatrix4(model.lines.matrixWorld).project(camera);
    const x = ((tmp.x + 1) / 2) * w, y = ((1 - tmp.y) / 2) * h;
    const [ox, oy] = model.map.labels?.[zone]?.offset ?? DEFAULT_OFFSETS[zone];
    let tx = w / 2 + ox * (w / 900) + (x - w / 2) * FOLLOW;
    let ty = h / 2 + oy * Math.min(1, h / 800) + (y - h / 2) * FOLLOW;
    const ew = el.offsetWidth;
    let left = ox < 0 ? tx - ew : ox > 0 ? tx : tx - ew / 2;
    left = Math.max(8, Math.min(w - ew - 8, left));
    ty = Math.max(110, Math.min(h - 90, ty));
    if (ox < 0) tx = left + ew; else if (ox > 0) tx = left; else tx = left + ew / 2;
    const sev = zoneSeverity(zone);
    const alert = sev === 'strained' || sev === 'critical';
    el.style.left = `${left}px`;
    el.style.top = `${ty - 12}px`;
    el.style.opacity = focus && focus !== zone ? 0.25 : alert || focus === zone ? 1 : 0.92;
    ln.setAttribute('points', `${x},${y} ${tx},${ty}`);
    ln.setAttribute('stroke', severityColor(sev));
    ln.setAttribute('stroke-opacity', focus && focus !== zone ? 0.15 : alert ? 0.95 : 0.7);
    ln.setAttribute('stroke-dasharray', alert ? '' : '3 3');
  }
}

// ---- animation ------------------------------------------------------------

const zoneColors = ZONES.map(() => new THREE.Color());
const zoneGain = new Float32Array(ZONES.length);
const shieldColor = new THREE.Color();
const scorch = [1, 0.35, 0.1];
let t = 0;

function frame() {
  t += 1 / 60;
  const bars = state?.Bars ?? {};
  const live = state && state.Overall !== 'nodata';
  const hullPct = live ? bars.hull ?? 100 : 100;
  const shieldPct = live ? bars.shield ?? 0 : 0;

  if (model) {
    ZONES.forEach((zone, zi) => {
      const sev = zoneSeverity(zone);
      zoneColors[zi].copy(SEV_THREE[sev] ?? SEV_THREE.nodata);
      let g = 1;
      if (sev === 'critical') g = 0.7 + 0.6 * Math.abs(Math.sin(t * 5));
      else if (sev === 'strained') g = 0.9 + 0.2 * Math.sin(t * 2);
      if (focus) g *= focus === zone ? 1.6 : 0.12;
      zoneGain[zi] = g;
    });
    const hullGain = focus ? 0.12 : 1;
    const dmg = 1 - hullPct / 100;
    const dim = 1 - dmg * 0.6;
    const col = model.lines.geometry.attributes.color;
    const c = col.array, zi = model.zoneIdx, nz = model.noise;
    for (let i = 0, n = zi.length; i < n; i++) {
      const z = zi[i];
      const base = z === HULL ? HULL_COLOR : zoneColors[z];
      const g = z === HULL ? hullGain : zoneGain[z];
      let r = base.r, gg = base.g, b = base.b;
      if (nz[i] < dmg * 0.9) {
        const f = Math.sin(t * 9 + nz[i] * 50) > 0.6 ? 1 : 0.4;
        r = scorch[0] * f; gg = scorch[1] * f; b = scorch[2] * f;
      } else {
        r *= dim; gg *= dim; b *= dim;
      }
      c[i * 3] = r * g; c[i * 3 + 1] = gg * g; c[i * 3 + 2] = b * g;
    }
    col.needsUpdate = true;
    levelColor(dmg < 0.3 ? 0 : dmg < 0.6 ? (dmg - 0.3) / 0.3 : 1 + (dmg - 0.6) / 0.4, model.solidMat.color);
    placeTags();
  }

  if (live) levelColor(shieldLevel(shieldPct), shieldColor);
  else shieldColor.copy(SEV_THREE.nodata);
  const flicker = live && shieldPct < 25 ? 0.6 + 0.4 * Math.sin(t * 12) : 1;
  shells.forEach((s, i) => {
    const on = live ? shieldPct > (100 * i) / shells.length : i === 0;
    s.material.color.copy(shieldColor);
    s.material.opacity = on ? s.userData.op * (0.35 + 0.65 * (live ? shieldPct : 30) / 100) * flicker : 0;
    s.rotation.y += 0.0015 * (i % 2 ? -1 : 1);
  });

  controls.update();
  renderer.render(scene, camera);
  requestAnimationFrame(frame);
}

window.addEventListener('resize', () => {
  const w = stage.clientWidth, h = stage.clientHeight;
  renderer.setSize(w, h);
  camera.aspect = w / h;
  camera.updateProjectionMatrix();
});

// ---- header and bars ------------------------------------------------------

const BARS = [
  ['hull', 'HULL'], ['shield', 'SHIELD'], ['fuel', 'FUEL'],
  ['power', 'POWER DRAW'], ['life', 'LIFE SUPPORT'], ['satisfaction', 'SATISFACTION'],
];

// barSeverity colours a bar by the zone that owns its figure; hull and shield
// share DEFENSE, so they only take its colour when a reason names them.
function barSeverity(key) {
  const owner = { fuel: 'fuel', power: 'core', life: 'life' }[key];
  if (owner) return zoneSeverity(owner);
  if (key === 'hull' || key === 'shield') {
    const def = state?.Zones?.find(z => z.Zone === 'defense');
    return def?.Reasons?.some(r => r.startsWith(key)) ? def.Severity : 'ok';
  }
  return 'ok';
}

function renderHeader() {
  const word = $('word');
  word.textContent = state.Word || 'NO DATA';
  word.style.color = state.Overall === 'ok' ? 'var(--good)' : severityColor(state.Overall);
  $('station').textContent = (state.station || 'Conjunction Junction').toUpperCase();
  const at = state.CapturedAt ? new Date(state.CapturedAt) : null;
  const hasReading = state.Overall !== 'nodata' && at && at.getFullYear() > 1;
  const mins = hasReading ? (Date.now() - at) / 60000 : 0;
  $('reading').textContent = hasReading
    ? `LAST READING ${at.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false })} (${ago(mins)} ago)`
    : 'NO READINGS CAPTURED YET';
  const stale = $('stale');
  stale.hidden = !(hasReading && state.Stale);
  stale.textContent = `STALE · NO NEW READING FOR ${ago(mins).toUpperCase()} · FIGURES MAY BE OUT OF DATE`;

  const host = $('bars');
  host.replaceChildren(...BARS.map(([key, label]) => {
    const v = state.Bars?.[key];
    const d = document.createElement('div');
    d.className = 'bar';
    const shown = hasReading && v != null;
    d.style.color = shown ? severityColor(barSeverity(key)) : severityColor('nodata');
    d.innerHTML = `<div class="lbl"><span>${label}</span><span>${shown ? `${Math.round(v)}%` : '—'}</span></div>`
      + `<div class="track"><div class="fill" style="width:${shown ? Math.max(0, Math.min(100, v)) : 0}%"></div></div>`;
    return d;
  }));
}

// ---- popups ---------------------------------------------------------------

const TRENDS = {
  core: [['POWER DRAW %', 'PowerDrawPct', '%']],
  life: [['LIFE SUPPORT MARGIN %', 'LifeMarginPct', '%']],
  fuel: [['FUEL %', 'FuelPct', '%']],
  production: [['BACKLOG TICKS', 'BacklogTicks', '']],
  tourism: [['TOURISM CR/H', 'TourismPerHour', '']],
  defense: [['SHIELD %', 'ShieldPct', '%'], ['HULL %', 'HullPct', '%']],
};

function sparkline(key, unit, color) {
  const end = Date.now(), start = end - TREND_HOURS * 3600_000;
  const pts = history
    .map(p => [Date.parse(p.At), p[key]])
    .filter(([at, v]) => v != null && Number.isFinite(v) && at >= start);
  if (pts.length < 2) {
    return `<div class="none">trend builds as hourly readings arrive (${pts.length} so far)</div>`;
  }
  const vs = pts.map(p => p[1]);
  let lo = Math.min(...vs), hi = Math.max(...vs);
  const span = Math.max(hi - lo, Math.abs(hi) * 0.1, 1);
  const mid = (lo + hi) / 2;
  lo = mid - span / 2 - span * 0.1;
  hi = mid + span / 2 + span * 0.1;
  const W = 300, H = 36;
  const xy = pts.map(([at, v]) => [((at - start) / (end - start)) * W, H - ((v - lo) / (hi - lo)) * H]);
  const line = xy.map(([x, y]) => `${x.toFixed(1)},${y.toFixed(1)}`).join(' ');
  const area = `${xy[0][0].toFixed(1)},${H} ${line} ${xy.at(-1)[0].toFixed(1)},${H}`;
  const last = xy.at(-1);
  return `<svg viewBox="0 0 ${W} ${H}" preserveAspectRatio="none">`
    + `<polygon points="${area}" fill="${color}" fill-opacity=".12"/>`
    + `<polyline points="${line}" fill="none" stroke="${color}" stroke-width="1.4" vector-effect="non-scaling-stroke"/>`
    + `<circle cx="${last[0].toFixed(1)}" cy="${last[1].toFixed(1)}" r="2.2" fill="${color}"/></svg>`
    + `<div class="tl"><span>-48h · min ${num(Math.min(...vs))}${unit} · max ${num(Math.max(...vs))}${unit}</span><span>now ${num(vs.at(-1))}${unit}</span></div>`;
}

const row = (k, v) => `<div class="row"><span>${esc(k)}</span><span>${esc(v)}</span></div>`;

function zoneFigures(zone) {
  const b = state.base ?? {};
  const p = state.passengers;
  switch (zone) {
    case 'core':
      return row('Power draw', `${num(b.PowerDraw)} / ${num(b.PowerSupply)} (${Math.round(pctOf(b.PowerDraw, b.PowerSupply))}%)`)
        + row('Battery', b.BatteryCapacity > 0 ? `${num(b.BatteryStored)} / ${num(b.BatteryCapacity)}` : 'none built');
    case 'life':
      return row('Supply / demand', `${num(b.LifeSupportSupply)} / ${num(b.LifeSupportDemand)} (${Math.round(pctOf(b.LifeSupportSupply, b.LifeSupportDemand))}%)`);
    case 'fuel':
      return row('Reserve', `${num(b.FuelReserve)} / ${num(b.FuelCapacity)} (${Math.round(pctOf(b.FuelReserve, b.FuelCapacity))}%)`);
    case 'tourism': {
      let s = '';
      if (p) {
        s += `<blockquote>${esc(p.MarketConditions || 'no market conditions reported')}</blockquote>`
          + row('Lounge seats', `${num(p.LoungeOccupancy)} / ${num(p.LoungeCapacity)} occupied`)
          + row('Waiting passengers', num(p.Waiting))
          + row('Demand', p.DemandLevel || 'unknown')
          + row('Fare', `×${(p.FareSurge ?? 0).toFixed(2)}`);
      } else {
        s += '<div class="none">no passenger reading yet</div>';
      }
      return s + row('Tourism', state.tourism_per_hour == null ? 'rate pending (needs 2 readings)' : `${num(state.tourism_per_hour)} cr/h`)
        + row('Revenue to date', `${num(b.TourismRevenue)} cr`)
        + row('Leisure / dining', `${num(b.LeisurePoints)} / ${num(b.DiningPoints)}`)
        + row('Satisfaction', `${num(b.SatisfactionPct)}%`);
    }
    case 'defense':
      return row('Weapons', `${num(b.WeaponDPS)} dps · reach ${num(b.WeaponReach)}`)
        + row('Armor', num(b.Armor))
        + row('Shield layers', num(state.ShieldShells))
        + row('Shield', `${num(b.Shield)} / ${num(b.MaxShield)} (${Math.round(pctOf(b.Shield, b.MaxShield))}%)`)
        + row('Hull', `${num(b.Hull)} / ${num(b.MaxHull)} (${Math.round(pctOf(b.Hull, b.MaxHull))}%)`);
  }
  return row('Status', state.Zones.find(z => z.Zone === zone)?.Brief ?? '');
}

function facilityTable(facs) {
  if (!facs?.length) return '<div class="none">no facilities in this zone</div>';
  const prod = facs.some(f => f.Recipe);
  const maint = f => (f.MaintenanceSatisfied == null ? '<span class="none">—</span>'
    : f.MaintenanceSatisfied ? 'ok' : '<span class="unmet">UNMET</span>');
  const head = '<tr><th>FACILITY</th><th class="n">LV</th><th>MAINT</th>'
    + (prod ? '<th>RECIPE</th><th class="n">RUNS</th><th class="n">BACKLOG</th><th class="n">/HR</th>' : '') + '</tr>';
  const body = facs.map(f => `<tr><td>${esc(f.Name || f.Type)}</td><td class="n">${num(f.Level)}</td><td>${maint(f)}</td>`
    + (prod ? `<td>${esc(f.Recipe || '—')}</td><td class="n">${f.Recipe ? num(f.QueuedRuns) : ''}</td>`
      + `<td class="n">${f.Recipe ? num(f.BacklogTicks) : ''}</td><td class="n">${f.Recipe ? (f.ItemsPerHour ?? 0).toFixed(1) : ''}</td>` : '')
    + '</tr>').join('');
  return `<table>${head}${body}</table>`;
}

function openPopup(zone) {
  openZone = zone;
  renderPopup();
}

function renderPopup() {
  const pop = $('pop');
  if (!openZone || !state) {
    pop.hidden = true;
    return;
  }
  const zs = state.Zones?.find(z => z.Zone === openZone);
  if (!zs) return;
  const color = severityColor(zs.Severity);
  pop.hidden = false;
  pop.style.color = color;
  pop.style.width = (zs.Facilities ?? []).some(f => f.Recipe) ? '600px' : '';
  // Open on the side away from the zone's label so the label stays visible.
  const [ox] = model?.map.labels?.[openZone]?.offset ?? DEFAULT_OFFSETS[openZone] ?? [0];
  pop.classList.toggle('left', ox > 0);
  $('popt').textContent = zs.Label;
  let html = `<div class="sev">${esc(zs.Severity.toUpperCase())}</div>`;
  if (zs.Reasons?.length) html += `<ul class="reasons">${zs.Reasons.map(r => `<li>${esc(r)}</li>`).join('')}</ul>`;
  if (state.Overall !== 'nodata') html += zoneFigures(openZone);
  for (const [label, key, unit] of TRENDS[openZone] ?? []) {
    html += `<h5>${label} · LAST ${TREND_HOURS}H</h5><div class="trend">${sparkline(key, unit, color)}</div>`;
  }
  html += `<h5>FACILITIES</h5>${facilityTable(zs.Facilities)}`;
  $('popb').innerHTML = html;
}

$('popx').onclick = () => {
  openZone = null;
  renderPopup();
};

// ---- data -----------------------------------------------------------------

async function getJSON(url) {
  const res = await fetch(url, { cache: 'no-store' });
  if (!res.ok) throw new Error(`${url}: ${res.status}`);
  return res.json();
}

async function pollState() {
  try {
    state = await getJSON('/api/state');
    $('link').hidden = true;
  } catch (err) {
    $('link').hidden = false;
    console.warn('state', err);
    return;
  }
  renderHeader();
  renderTagText();
  rebuildShells();
  renderPopup();
}

async function pollHistory() {
  try {
    history = await getJSON(`/api/history?hours=${TREND_HOURS}`);
  } catch (err) {
    console.warn('history', err);
    return;
  }
  renderPopup();
}

function storedModel() {
  try {
    return localStorage.getItem(MODEL_KEY);
  } catch {
    return null;
  }
}

function rememberModel(id) {
  try {
    localStorage.setItem(MODEL_KEY, id);
  } catch {
    // Storage blocked: the choice just lasts for this visit.
  }
}

async function initSwitcher(params) {
  let models = [];
  try {
    models = await getJSON('/api/models');
  } catch (err) {
    showMsg('MODEL LIST UNAVAILABLE');
    console.error('models', err);
    return;
  }
  const sel = $('switch');
  for (const m of models) sel.add(new Option(m.label, m.id));
  const has = id => models.some(m => m.id === id);
  const pick = [params.get('model'), storedModel(), models.find(m => m.default)?.id, models[0]?.id].find(id => id && has(id));
  const choose = id => {
    sel.value = id;
    $('art').src = `/models/${encodeURIComponent(id)}/keyed.png`;
    loadModel(id);
  };
  sel.onchange = () => {
    rememberModel(sel.value);
    choose(sel.value);
  };
  if (pick) choose(pick);
}

$('art').onerror = e => (e.target.style.visibility = 'hidden');
$('art').onload = e => (e.target.style.visibility = '');

const params = new URLSearchParams(location.search);
frame();
await Promise.all([pollState(), pollHistory(), initSwitcher(params)]);
if (params.get('zone')) openPopup(params.get('zone'));
setInterval(pollState, STATE_EVERY);
setInterval(pollHistory, HISTORY_EVERY);
setInterval(() => state && renderHeader(), 30_000);
