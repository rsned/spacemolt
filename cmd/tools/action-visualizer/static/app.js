// Action Space Visualizer — D3 Radial Dendrogram

// Category color scale.
const categoryColors = {
  navigation:  '#58a6ff',
  mining:      '#d29922',
  trading:     '#3fb950',
  exchange:    '#2ea043',
  combat:      '#f85149',
  salvage:     '#bc8cff',
  ship:        '#79c0ff',
  cargo:       '#a5d6ff',
  storage:     '#7ee787',
  crafting:    '#f0883e',
  missions:    '#d2a8ff',
  faction:     '#ff7b72',
  social:      '#ffa657',
  insurance:   '#56d364',
  exploration: '#e3b341',
};

let currentData = null;
let debounceTimer = null;

// ===== State Building =====

function buildGameContext() {
  const docked = document.querySelector('input[name="dock-state"]:checked').value === 'docked';
  const inCombat = document.getElementById('toggle-combat').checked;
  const inTransit = document.getElementById('toggle-transit').checked;

  const fuel = parseInt(document.getElementById('slider-fuel').value);
  const hull = parseInt(document.getElementById('slider-hull').value);
  const cargoUsed = parseInt(document.getElementById('slider-cargo').value);
  const credits = parseInt(document.getElementById('slider-credits').value);
  const cargoItems = parseInt(document.getElementById('num-cargo-items').value) || 0;
  const nearby = parseInt(document.getElementById('num-nearby').value) || 0;
  const wrecks = parseInt(document.getElementById('num-wrecks').value) || 0;
  const storedShips = parseInt(document.getElementById('num-stored-ships').value) || 0;
  const hasFaction = document.getElementById('toggle-faction').checked;
  const towingWreck = document.getElementById('toggle-towing').checked;

  const poiType = document.getElementById('select-poi-type').value;
  const numPOIs = parseInt(document.getElementById('num-pois').value) || 0;
  const numConns = parseInt(document.getElementById('num-connections').value) || 0;

  // Build synthetic POI list.
  const pois = [];
  if (numPOIs > 0) {
    pois.push({ ID: 'current_poi', Name: 'Current Location', Type: poiType, HasBase: poiType === 'station' });
    for (let i = 1; i < numPOIs; i++) {
      const isStation = i === 1;
      pois.push({
        ID: `poi_${i}`,
        Name: isStation ? `Station ${i}` : `POI ${i}`,
        Type: isStation ? 'station' : 'planet',
        HasBase: isStation,
      });
    }
  }

  // Build synthetic connection list.
  const connections = [];
  for (let i = 0; i < numConns; i++) {
    connections.push({ SystemID: `sys_${i}`, Name: `System ${i}`, Distance: 3 + i });
  }

  return {
    Docked: docked,
    InTransit: inTransit,
    InCombat: inCombat,
    InBattle: false,
    CurrentPOIType: poiType,
    CurrentPOIID: 'current_poi',
    SystemPOIs: pois,
    Connections: connections,
    Fuel: fuel,
    MaxFuel: 100,
    Hull: hull,
    MaxHull: 100,
    CargoUsed: cargoUsed,
    CargoCapacity: 50,
    Credits: credits,
    Modules: [],
    TowingWreck: towingWreck,
    HasFaction: hasFaction,
    FactionRank: '',
    IsCloaked: false,
    NearbyPlayerCount: nearby,
    WreckCount: wrecks,
    StoredShipCount: storedShips,
    CargoItemCount: cargoItems,
  };
}

// ===== API Calls =====

async function evaluate() {
  const gc = buildGameContext();
  try {
    const resp = await fetch('/api/evaluate', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(gc),
    });
    const data = await resp.json();
    currentData = data;
    updateMetrics(data.stats);
    renderTree(data.tree);
  } catch (err) {
    console.error('Evaluate failed:', err);
  }
}

function debouncedEvaluate() {
  clearTimeout(debounceTimer);
  debounceTimer = setTimeout(evaluate, 50);
}

// ===== Metrics =====

function updateMetrics(stats) {
  document.getElementById('m-branching').textContent = stats.branching_factor;
  document.getElementById('m-valid').textContent = stats.valid_actions;
  document.getElementById('m-total').textContent = stats.total_actions;
  document.getElementById('m-pruned-pct').textContent = stats.pruned_percent.toFixed(1) + '%';

  // Count active categories (categories with at least 1 valid action).
  let activeCats = 0;
  if (stats.by_category) {
    for (const cat of Object.values(stats.by_category)) {
      if (cat.valid > 0) activeCats++;
    }
  }
  document.getElementById('m-categories').textContent = activeCats + ' / ' + Object.keys(stats.by_category || {}).length;

  // Top pruning reason.
  const topPrune = document.getElementById('m-top-prune');
  if (stats.top_pruning_reasons && stats.top_pruning_reasons.length > 0) {
    const top = stats.top_pruning_reasons[0];
    topPrune.textContent = `${top.precondition} (${top.count})`;
  } else {
    topPrune.textContent = 'none';
  }
}

// ===== Tree Rendering =====

function renderTree(treeData) {
  const container = document.getElementById('graph-panel');
  const width = container.clientWidth;
  const height = container.clientHeight;
  const radius = Math.min(width, height) / 2 - 80;

  // Clear previous.
  d3.select('#graph-svg').selectAll('*').remove();

  const svg = d3.select('#graph-svg')
    .attr('viewBox', [-width / 2, -height / 2, width, height]);

  const root = d3.hierarchy(treeData);

  // Cluster layout in radial coordinates.
  const cluster = d3.cluster()
    .size([2 * Math.PI, radius])
    .separation((a, b) => {
      // Give more space between categories.
      if (a.parent !== b.parent) return 2;
      return 1;
    });

  cluster(root);

  // Draw links.
  const linkGroup = svg.append('g').attr('fill', 'none');

  linkGroup.selectAll('path')
    .data(root.links())
    .join('path')
    .attr('class', d => {
      if (d.target.data.valid === false) return 'link-pruned';
      if (d.target.children && d.target.data.validCount === 0) return 'link-pruned';
      return 'link-valid';
    })
    .attr('stroke', d => {
      const cat = d.target.data.category || d.source.data.category;
      return categoryColors[cat] || '#30363d';
    })
    .attr('d', d3.linkRadial()
      .angle(d => d.x)
      .radius(d => d.y));

  // Draw nodes.
  const nodeGroup = svg.append('g');

  const nodes = nodeGroup.selectAll('g')
    .data(root.descendants())
    .join('g')
    .attr('transform', d => `rotate(${d.x * 180 / Math.PI - 90}) translate(${d.y},0)`)
    .attr('class', d => {
      if (d.depth === 0) return 'node-root';
      if (d.depth === 1) return 'node-category';
      return d.data.valid ? 'node-action node-valid' : 'node-action node-pruned';
    });

  // Circles.
  nodes.append('circle')
    .attr('r', d => {
      if (d.depth === 0) return 18;
      if (d.depth === 1) {
        // Size by proportion of valid children.
        const total = d.data.totalCount || 1;
        const valid = d.data.validCount || 0;
        return 6 + (valid / total) * 8;
      }
      return d.data.valid ? 4 : 3;
    })
    .attr('fill', d => {
      if (d.depth === 0) return '#30363d';
      if (d.depth === 1) {
        const color = categoryColors[d.data.category] || '#8b949e';
        return d.data.validCount > 0 ? color : '#21262d';
      }
      if (d.data.valid) return categoryColors[d.data.category] || '#58a6ff';
      return '#21262d';
    })
    .attr('stroke', d => {
      if (d.depth === 0) return '#58a6ff';
      if (d.depth === 1) return categoryColors[d.data.category] || '#30363d';
      return 'none';
    });

  // Labels.
  nodes.append('text')
    .attr('dy', '0.31em')
    .attr('x', d => {
      if (d.depth === 0) return 0;
      return d.x < Math.PI === !d.children ? 8 : -8;
    })
    .attr('text-anchor', d => {
      if (d.depth === 0) return 'middle';
      return d.x < Math.PI === !d.children ? 'start' : 'end';
    })
    .attr('transform', d => {
      if (d.depth === 0) return '';
      return d.x >= Math.PI ? 'rotate(180)' : null;
    })
    .attr('fill', d => {
      if (d.depth === 0) return '#f0f6fc';
      if (d.depth === 1) {
        return d.data.validCount > 0 ? '#c9d1d9' : '#484f58';
      }
      return d.data.valid ? '#c9d1d9' : '#484f58';
    })
    .text(d => {
      if (d.depth === 0) return d.data.name;
      if (d.depth === 1) return `${d.data.name} (${d.data.validCount}/${d.data.totalCount})`;
      return d.data.name;
    })
    .style('font-size', d => {
      if (d.depth === 0) return '14px';
      if (d.depth === 1) return '11px';
      return '9px';
    })
    .style('font-weight', d => d.depth <= 1 ? '600' : '400');

  // Tooltips on action leaves.
  const tooltip = document.getElementById('tooltip');

  nodes.filter(d => d.depth === 2)
    .on('mouseenter', (event, d) => {
      const data = d.data;
      let html = `<div class="tt-name">${data.name}</div>`;
      if (data.summary) html += `<div class="tt-summary">${data.summary}</div>`;

      if (data.valid) {
        html += `<div class="tt-status valid">VALID</div>`;
        if (data.branchingCount > 1) {
          html += `<div class="tt-targets">Branching: ${data.branchingCount} targets</div>`;
        }
        if (data.targets && data.targets.length > 0 && data.targets.length <= 6) {
          html += `<div class="tt-targets">Targets: ${data.targets.join(', ')}</div>`;
        }
      } else {
        html += `<div class="tt-status pruned">PRUNED</div>`;
        if (data.failedChecks && data.failedChecks.length > 0) {
          html += `<div class="tt-failed">Failed: ${data.failedChecks.join(', ')}</div>`;
        }
      }

      tooltip.innerHTML = html;
      tooltip.classList.add('visible');

      // Position tooltip near cursor.
      const rect = container.getBoundingClientRect();
      tooltip.style.left = (event.clientX - rect.left + 12) + 'px';
      tooltip.style.top = (event.clientY - rect.top - 10) + 'px';
    })
    .on('mousemove', (event) => {
      const rect = container.getBoundingClientRect();
      tooltip.style.left = (event.clientX - rect.left + 12) + 'px';
      tooltip.style.top = (event.clientY - rect.top - 10) + 'px';
    })
    .on('mouseleave', () => {
      tooltip.classList.remove('visible');
    });

  // Build legend.
  renderLegend(treeData);
}

function renderLegend(treeData) {
  const legend = document.getElementById('legend');
  let html = '';

  for (const child of treeData.children || []) {
    const cat = child.category;
    const color = categoryColors[cat] || '#8b949e';
    const opacity = child.validCount > 0 ? 1 : 0.3;
    html += `<div class="legend-item" style="opacity:${opacity}">
      <div class="legend-dot" style="background:${color}"></div>
      <span>${cat} (${child.validCount}/${child.totalCount})</span>
    </div>`;
  }

  legend.innerHTML = html;
}

// ===== State Constraint Enforcement =====

function enforceConstraints() {
  const isDocked = document.querySelector('input[name="dock-state"]:checked').value === 'docked';
  const combatRow = document.getElementById('row-combat');
  const transitRow = document.getElementById('row-transit');
  const combatToggle = document.getElementById('toggle-combat');
  const transitToggle = document.getElementById('toggle-transit');

  // Can't be in combat or transit while docked.
  if (isDocked) {
    combatToggle.checked = false;
    transitToggle.checked = false;
    combatRow.classList.add('disabled');
    transitRow.classList.add('disabled');
  } else {
    combatRow.classList.remove('disabled');
    transitRow.classList.remove('disabled');
  }

  // Can't be in combat and in transit.
  if (combatToggle.checked) {
    transitToggle.checked = false;
    transitRow.classList.add('disabled');
  } else if (transitToggle.checked) {
    combatToggle.checked = false;
    combatRow.classList.add('disabled');
  }
}

// ===== Slider Value Display =====

function updateSliderLabels() {
  const fuel = document.getElementById('slider-fuel').value;
  document.getElementById('val-fuel').textContent = `${fuel} / 100`;

  const hull = document.getElementById('slider-hull').value;
  document.getElementById('val-hull').textContent = `${hull} / 100`;

  const cargo = document.getElementById('slider-cargo').value;
  document.getElementById('val-cargo').textContent = `${cargo} / 50`;

  const credits = document.getElementById('slider-credits').value;
  document.getElementById('val-credits').textContent = parseInt(credits).toLocaleString();
}

// ===== Event Binding =====

function init() {
  // Radio buttons.
  document.querySelectorAll('input[name="dock-state"]').forEach(el => {
    el.addEventListener('change', () => { enforceConstraints(); debouncedEvaluate(); });
  });

  // Checkboxes.
  const checkboxes = ['toggle-combat', 'toggle-transit', 'toggle-faction', 'toggle-towing'];
  for (const id of checkboxes) {
    document.getElementById(id).addEventListener('change', () => {
      enforceConstraints();
      debouncedEvaluate();
    });
  }

  // Sliders.
  const sliders = ['slider-fuel', 'slider-hull', 'slider-cargo', 'slider-credits'];
  for (const id of sliders) {
    document.getElementById(id).addEventListener('input', () => {
      updateSliderLabels();
      debouncedEvaluate();
    });
  }

  // Number inputs.
  const numbers = ['num-cargo-items', 'num-nearby', 'num-wrecks', 'num-stored-ships', 'num-pois', 'num-connections'];
  for (const id of numbers) {
    document.getElementById(id).addEventListener('input', debouncedEvaluate);
  }

  // Select.
  document.getElementById('select-poi-type').addEventListener('change', debouncedEvaluate);

  // Handle window resize.
  window.addEventListener('resize', () => {
    if (currentData) renderTree(currentData.tree);
  });

  // Initial state.
  enforceConstraints();
  updateSliderLabels();
  evaluate();
}

init();
