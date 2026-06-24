const app = document.getElementById('app');
const statsEl = document.getElementById('stats');
let view = 'matrix';
let page = 1; // current matrix page (1-based); reset to 1 on category/search change

const fmt = (n) => (n == null || Number.isNaN(n)) ? '—' : Number(n).toLocaleString(undefined, { maximumFractionDigits: 2 });
function relTime(ts) {
  if (!ts) return '—';
  const d = (Date.now() - new Date(ts).getTime()) / 1000;
  if (d < 60) return 'just now';
  if (d < 3600) return Math.floor(d / 60) + ' min ago';
  if (d < 86400) return Math.floor(d / 3600) + ' hr ago';
  if (d < 604800) return Math.floor(d / 86400) + ' days ago';
  return ts.slice(0, 10);
}
async function getJSON(url) {
  const res = await fetch(url);
  if (!res.ok) throw new Error(res.status + ' ' + (await res.text()));
  return res.json();
}
function showError(e) { app.innerHTML = '<div id="error">Error: ' + (e.message || e) + '</div>'; }

async function loadStats() {
  try {
    const s = await getJSON('/api/stats');
    statsEl.textContent =
      `stations ${s.station_count} · items ${s.item_count} · orders ${s.order_count} · ohlcv ${s.ohlcv_count} · latest ${s.latest_capture || '—'}`;
  } catch (e) { /* stats are best-effort */ }
}

async function loadCategories() {
  const sel = document.getElementById('category');
  // Derive from the first matrix page's items (categories self-populate as captures land).
  // Lightweight: just fetch a wide matrix and collect distinct categories.
  try {
    const m = await getJSON('/api/matrix?page=1&limit=500');
    const cats = [...new Set(m.items.map(i => i.category).filter(Boolean))].sort();
    cats.forEach(c => { const o = document.createElement('option'); o.value = c; o.textContent = c; sel.appendChild(o); });
  } catch (e) { /* ignore */ }
}

async function renderMatrix() {
  const cat = document.getElementById('category').value;
  const q = document.getElementById('search').value;
  const params = new URLSearchParams({ category: cat, q, page: String(page), limit: '50' });
  try {
    const m = await getJSON('/api/matrix?' + params);
    if (!m.items.length) { app.innerHTML = '<p>No items match.</p>'; renderPager(0, 0, 50); return; }
    const cols = ['<th>item</th>'].concat(m.stations.map(s => `<th>${s.station_name}</th>`)).join('');
    const rows = m.items.map(it =>
      `<tr><td class="item">${it.item_id}<br><small>${it.item_name || ''} · ${it.category || ''}</small></td>` +
      it.cells.map(c => `<td class="cell" data-item="${it.item_id}" data-station="${c.station_id}">${
        (c.has_sell || c.has_buy)
          ? `${c.has_sell ? '<span class="sell">'+fmt(c.best_sell)+'</span>' : ''} ${c.has_buy ? '<span class="buy">'+fmt(c.best_buy)+'</span>' : ''}<br>${c.has_sell?'vol '+fmt(c.volume):''} · ${relTime(c.captured_at)}`
          : '<span class="sparse">—</span>'
      }</td>`).join('')).join('');
    app.innerHTML = `<table><thead><tr>${cols}</tr></thead><tbody>${rows}</tbody></table>`;
    renderPager(m.page, m.total_items, m.limit);
    app.querySelectorAll('td.cell').forEach(td => {
      td.addEventListener('click', () => openCell(td.dataset.item, td.dataset.station));
    });
  } catch (e) { showError(e); }
}

function renderPager(currentPage, total, limit) {
  const pages = Math.ceil(total / limit);
  const pager = document.getElementById('pager');
  if (pages <= 1) { pager.textContent = `${total} items`; return; }
  pager.innerHTML = '';
  const prev = document.createElement('button');
  prev.type = 'button';
  prev.textContent = '‹ Prev';
  prev.disabled = currentPage <= 1;
  const info = document.createElement('span');
  info.textContent = ` page ${currentPage}/${pages} · ${total} items `;
  const next = document.createElement('button');
  next.type = 'button';
  next.textContent = 'Next ›';
  next.disabled = currentPage >= pages;
  prev.addEventListener('click', () => { if (page > 1) { page--; renderMatrix(); } });
  next.addEventListener('click', () => { if (page < pages) { page++; renderMatrix(); } });
  pager.append(prev, info, next);
}

async function openCell(item, station) {
  const body = document.getElementById('cell-body');
  body.textContent = 'Loading…';
  document.getElementById('cell-dialog').showModal();
  try {
    const orders = await getJSON(`/api/station/${encodeURIComponent(station)}/orders?item=${encodeURIComponent(item)}`);
    if (!orders.length) { body.innerHTML = '<p>No orders.</p>'; return; }
    const rows = orders.map(o =>
      `<tr><td>${o.side}</td><td>${fmt(o.price_each)}</td><td>${fmt(o.quantity)}</td><td>${o.source || ''}</td></tr>`).join('');
    body.innerHTML = `<h3>${item} @ ${station}</h3><table><thead><tr><th>side</th><th>price</th><th>qty</th><th>source</th></tr></thead><tbody>${rows}</tbody></table>`;
  } catch (e) { body.innerHTML = '<p id="error">' + (e.message || e) + '</p>'; }
}

async function renderPrice() {
  const item = document.getElementById('price-item').value.trim();
  if (!item) { app.innerHTML = '<p>Enter an item id above (e.g. iron_ore).</p>'; return; }
  try {
    const pts = await getJSON(`/api/item/${encodeURIComponent(item)}/history?limit=100`);
    if (!pts.length) { app.innerHTML = `<p>No history for ${item}.</p>`; return; }
    const rows = pts.map(p =>
      `<tr><td>${p.bucket_utc}</td><td>${p.station_name}</td><td>${p.side}</td><td class="sell">${fmt(p.vwap)}</td><td>${fmt(p.high)}</td><td>${fmt(p.low)}</td><td>${fmt(p.volume)}</td><td>${p.trade_count}</td></tr>`).join('');
    app.innerHTML = `<h3>${item} — VWAP over time</h3>
      <table><thead><tr><th>bucket</th><th>station</th><th>side</th><th>vwap</th><th>high</th><th>low</th><th>vol</th><th>trades</th></tr></thead><tbody>${rows}</tbody></table>
      <p><small>VWAP is the robust series; high/low reveal thin-item noise.</small></p>`;
  } catch (e) { showError(e); }
}

async function renderHealth() {
  try {
    const health = await getJSON('/api/captures');
    if (!health.length) { app.innerHTML = '<p>No captures yet.</p>'; return; }
    const blocks = health.map(h =>
      `<h3>${h.station_name} <small>(${h.count} captures, ${h.earliest} → ${h.latest})</small></h3>
       <ul>${h.capture_times.map(t => `<li>${t}</li>`).join('')}</ul>`).join('');
    app.innerHTML = blocks;
  } catch (e) { showError(e); }
}

async function renderOpps() {
  try {
    const opps = await getJSON('/api/opportunities?limit=100');
    if (!opps.length) { app.innerHTML = '<p>No opportunities. Run <code>arbitrage-scanner scan</code>.</p>'; return; }
    const rows = opps.map(o =>
      `<tr><td class="item">${o.item_id}<br><small>${o.item_name || ''}</small></td>
       <td>${o.from_station_name || o.from_station_id} → ${o.to_station_name || o.to_station_id}<br><small>${o.from_system_name || ''} → ${o.to_system_name || ''}</small></td>
       <td>${fmt(o.buy_price)}</td><td>${fmt(o.sell_price)}</td><td>${fmt(o.quantity)}</td>
       <td class="sell">${fmt(o.gross_profit)}</td><td>${o.status}</td><td>${relTime(o.expires_at)}</td></tr>`).join('');
    app.innerHTML = `<h3>Arbitrage opportunities</h3>
      <table><thead><tr><th>item</th><th>from → to</th><th>buy</th><th>sell</th><th>qty</th><th>gross</th><th>status</th><th>expires</th></tr></thead><tbody>${rows}</tbody></table>`;
  } catch (e) { showError(e); }
}

function showView(v) {
  view = v;
  document.querySelectorAll('.tab').forEach(t => t.classList.toggle('active', t.dataset.view === v));
  document.getElementById('matrix-controls').classList.toggle('hidden', v !== 'matrix');
  document.getElementById('price-controls').classList.toggle('hidden', v !== 'price');
  if (v === 'matrix') renderMatrix();
  else if (v === 'price') renderPrice();
  else if (v === 'health') renderHealth();
  else if (v === 'opps') renderOpps();
}

document.querySelectorAll('.tab').forEach(t => t.addEventListener('click', () => showView(t.dataset.view)));
document.getElementById('refresh').addEventListener('click', () => { loadStats(); showView(view); });
document.getElementById('category').addEventListener('change', () => { page = 1; renderMatrix(); });
document.getElementById('search').addEventListener('input', debounce(() => { page = 1; renderMatrix(); }, 300));
document.getElementById('price-go').addEventListener('click', renderPrice);
document.getElementById('price-item').addEventListener('keydown', e => { if (e.key === 'Enter') renderPrice(); });
document.getElementById('cell-close').addEventListener('click', () => document.getElementById('cell-dialog').close());

function debounce(fn, ms) { let t; return (...a) => { clearTimeout(t); t = setTimeout(() => fn(...a), ms); }; }

showView('matrix');
loadStats();
loadCategories();
