// app.js - Main application logic for SpaceMolt Fleet Command UI

(function () {
  'use strict';

  // --- State ---
  var agentDataMap = {};      // id -> { agent, state }
  var currentView = 'fleet';  // 'fleet' or 'detail'
  var selectedAgentID = null;
  var refreshInterval = null;
  var sseHandle = null;
  var pollIntervalMs = 5000;  // Polling interval for fleet overview

  // --- DOM refs ---
  var fleetView = document.getElementById('fleet-view');
  var detailView = document.getElementById('detail-view');
  var agentGrid = document.getElementById('agent-grid');
  var connectionDot = document.getElementById('connection-status');
  var agentCountEl = document.getElementById('agent-count');
  var tickDisplayEl = document.getElementById('tick-display');
  var btnRefresh = document.getElementById('btn-refresh');
  var btnBack = document.getElementById('btn-back');

  // Detail view refs
  var detailAgentName = document.getElementById('detail-agent-name');
  var detailRole = document.getElementById('detail-role');
  var detailStatus = document.getElementById('detail-status');
  var shipGraphicContainer = document.getElementById('ship-graphic-container');
  var modulesList = document.getElementById('modules-list');
  var playerStats = document.getElementById('player-stats');
  var locationInfo = document.getElementById('location-info');
  var cargoList = document.getElementById('cargo-list');
  var actionLog = document.getElementById('action-log');

  // --- Init ---
  function init() {
    btnRefresh.addEventListener('click', refreshFleet);
    btnBack.addEventListener('click', showFleetView);

    // Check for hash-based routing
    if (window.location.hash && window.location.hash.startsWith('#agent/')) {
      var id = window.location.hash.substring(7);
      if (id) {
        selectedAgentID = id;
        currentView = 'detail';
      }
    }

    refreshFleet();
    startPolling();
  }

  // --- Polling ---
  function startPolling() {
    if (refreshInterval) clearInterval(refreshInterval);
    refreshInterval = setInterval(function () {
      if (currentView === 'fleet') {
        refreshFleet();
      } else if (currentView === 'detail' && selectedAgentID) {
        refreshDetail(selectedAgentID);
      }
    }, pollIntervalMs);
  }

  // --- Fleet view ---
  async function refreshFleet() {
    try {
      var data = await SpaceMoltAPI.getAllAgentStates();
      connectionDot.className = 'status-dot connected';

      // Update agent data map
      data.forEach(function (d) {
        var id = d.agent.id;
        agentDataMap[id] = d;
      });

      agentCountEl.textContent = data.length + ' agent' + (data.length !== 1 ? 's' : '');

      // Find max tick across agents
      var maxTick = 0;
      data.forEach(function (d) {
        if (d.state && d.state.CurrentTick > maxTick) {
          maxTick = d.state.CurrentTick;
        }
      });
      if (maxTick > 0) {
        tickDisplayEl.textContent = 'Tick: ' + maxTick;
      }

      if (currentView === 'fleet') {
        renderFleetGrid(data);
      } else if (currentView === 'detail' && selectedAgentID) {
        var entry = agentDataMap[selectedAgentID];
        if (entry) renderDetailView(entry);
      }
    } catch (err) {
      connectionDot.className = 'status-dot disconnected';
      console.error('Failed to refresh fleet:', err);
    }
  }

  function renderFleetGrid(data) {
    // Sort: running first, then crashed, then alphabetical
    data.sort(function (a, b) {
      var aRunning = a.agent.is_running !== false;
      var bRunning = b.agent.is_running !== false;
      if (aRunning !== bRunning) return bRunning ? 1 : -1;
      var aName = (a.agent.name || a.agent.id).toLowerCase();
      var bName = (b.agent.name || b.agent.id).toLowerCase();
      return aName < bName ? -1 : aName > bName ? 1 : 0;
    });

    agentGrid.innerHTML = '';
    data.forEach(function (d) {
      agentGrid.appendChild(createAgentTile(d));
    });
  }

  function createAgentTile(data) {
    var agent = data.agent;
    var state = data.state;
    var tile = document.createElement('div');
    tile.className = 'agent-tile';

    if (agent.has_crashed) tile.classList.add('crashed');
    if (state && state.InCombat) tile.classList.add('in-combat');

    // Status class
    var statusClass = 'status';
    var statusText = agent.status || 'unknown';
    if (agent.has_crashed) {
      statusClass += ' crashed';
      statusText = 'CRASHED';
    } else if (state && state.Traveling) {
      statusClass += ' traveling';
      statusText = 'TRAVELING';
    } else if (state && state.Doc) {
      statusClass += ' docked';
      statusText = 'DOCKED';
    }

    // Location
    var location = '--';
    if (state) {
      location = state.CurrentSystem || state.System.Name || '--';
      if (state.CurrentPOI) location += ' / ' + state.CurrentPOI;
    }

    // Ship stats
    var hullPct = 0, fuelPct = 0, cargoPct = 0;
    var hullText = '--', fuelText = '--', cargoText = '--';
    var credits = '--';

    if (state) {
      credits = formatCredits(state.Credits);

      if (state.Ship && state.Ship.max_hull > 0) {
        hullPct = state.Ship.hull / state.Ship.max_hull;
        hullText = Math.round(state.Ship.hull) + '/' + Math.round(state.Ship.max_hull);
      } else if (state.MaxHull > 0) {
        hullPct = state.Hull / state.MaxHull;
        hullText = Math.round(state.Hull) + '/' + Math.round(state.MaxHull);
      }

      if (state.Ship && state.Ship.max_fuel > 0) {
        fuelPct = state.Ship.fuel / state.Ship.max_fuel;
        fuelText = Math.round(state.Ship.fuel) + '/' + Math.round(state.Ship.max_fuel);
      } else if (state.MaxFuel > 0) {
        fuelPct = state.Fuel / state.MaxFuel;
        fuelText = Math.round(state.Fuel) + '/' + Math.round(state.MaxFuel);
      }

      if (state.Ship && state.Ship.cargo_capacity > 0) {
        cargoPct = state.Ship.cargo_used / state.Ship.cargo_capacity;
        cargoText = Math.round(state.Ship.cargo_used) + '/' + Math.round(state.Ship.cargo_capacity);
      }
    }

    // Action
    var actionText = agent.current_action || '--';

    tile.innerHTML =
      '<div class="tile-header">' +
        '<span class="tile-name">' + escapeHtml(agent.name || agent.id) + '</span>' +
        '<div class="tile-badges">' +
          '<span class="badge role">' + escapeHtml(agent.role || '--') + '</span>' +
          '<span class="badge ' + statusClass + '">' + escapeHtml(statusText) + '</span>' +
        '</div>' +
      '</div>' +
      '<div class="tile-location">&#x2316; <span class="system-name">' + escapeHtml(location) + '</span></div>' +
      '<div class="tile-stats">' +
        '<div class="tile-stat">' +
          '<span class="tile-stat-label">Credits</span>' +
          '<span class="tile-stat-value credits">' + credits + ' cr</span>' +
        '</div>' +
        '<div class="tile-stat">' +
          '<span class="tile-stat-label">Hull</span>' +
          '<span class="tile-stat-value">' + hullText + '</span>' +
          miniBar(hullPct, ShipGraphic.barColor(hullPct)) +
        '</div>' +
        '<div class="tile-stat">' +
          '<span class="tile-stat-label">Fuel</span>' +
          '<span class="tile-stat-value">' + fuelText + '</span>' +
          miniBar(fuelPct, '#22d3ee') +
        '</div>' +
        '<div class="tile-stat">' +
          '<span class="tile-stat-label">Cargo</span>' +
          '<span class="tile-stat-value">' + cargoText + '</span>' +
          miniBar(cargoPct, '#a855f7') +
        '</div>' +
      '</div>' +
      '<div class="tile-action">ACTION <span class="action-text">' + escapeHtml(actionText) + '</span></div>';

    tile.addEventListener('click', function () {
      showDetailView(agent.id);
    });

    return tile;
  }

  function miniBar(pct, color) {
    var w = Math.max(0, Math.min(100, pct * 100));
    return '<div class="mini-bar"><div class="mini-bar-fill" style="width:' + w + '%; background:' + color + ';"></div></div>';
  }

  // --- Detail View ---
  function showFleetView() {
    currentView = 'fleet';
    selectedAgentID = null;
    window.location.hash = '';
    fleetView.classList.add('active');
    detailView.classList.remove('active');
    if (sseHandle) { sseHandle.close(); sseHandle = null; }
    refreshFleet();
  }

  function showDetailView(agentID) {
    currentView = 'detail';
    selectedAgentID = agentID;
    window.location.hash = 'agent/' + agentID;
    fleetView.classList.remove('active');
    detailView.classList.add('active');

    // Render with cached data first
    var cached = agentDataMap[agentID];
    if (cached) renderDetailView(cached);

    // Fetch fresh data
    refreshDetail(agentID);
  }

  async function refreshDetail(agentID) {
    try {
      var agent = await SpaceMoltAPI.getAgent(agentID);
      var state = null;
      try { state = await SpaceMoltAPI.getAgentState(agentID); } catch (_) {}
      var history = [];
      try { history = await SpaceMoltAPI.getAgentHistory(agentID, 30); } catch (_) {}

      var data = { agent: agent, state: state, history: history };
      agentDataMap[agentID] = data;
      if (currentView === 'detail' && selectedAgentID === agentID) {
        renderDetailView(data);
      }
    } catch (err) {
      console.error('Failed to refresh detail for', agentID, err);
    }
  }

  function renderDetailView(data) {
    var agent = data.agent;
    var state = data.state;
    var history = data.history || [];

    // Header
    detailAgentName.textContent = agent.name || agent.id;
    detailRole.textContent = agent.role || '--';

    var statusText = agent.status || 'unknown';
    var statusClass = 'badge status';
    if (agent.has_crashed) { statusText = 'CRASHED'; statusClass += ' crashed'; }
    else if (state && state.Traveling) { statusText = 'TRAVELING'; statusClass += ' traveling'; }
    else if (state && state.Doc) { statusText = 'DOCKED'; statusClass += ' docked'; }
    detailStatus.textContent = statusText;
    detailStatus.className = statusClass;

    // Ship graphic
    if (state && state.Ship) {
      ShipGraphic.render(shipGraphicContainer, state.Ship, state);
    } else {
      shipGraphicContainer.innerHTML = '<div class="empty-text">No ship data</div>';
    }

    // Modules
    renderModules(state);

    // Player stats
    renderPlayerStats(state, agent);

    // Location
    renderLocation(state);

    // Cargo
    renderCargo(state);

    // Action log
    renderActionLog(history);
  }

  function renderModules(state) {
    modulesList.innerHTML = '';
    if (!state || !state.Ship || !state.Ship.modules || state.Ship.modules.length === 0) {
      modulesList.innerHTML = '<span class="module-slot-empty">No modules installed</span>';
      return;
    }

    // Count duplicates
    var counts = {};
    state.Ship.modules.forEach(function (m) {
      counts[m] = (counts[m] || 0) + 1;
    });

    Object.keys(counts).forEach(function (mod) {
      var tag = document.createElement('span');
      tag.className = 'module-tag';
      tag.textContent = mod + (counts[mod] > 1 ? ' x' + counts[mod] : '');
      modulesList.appendChild(tag);
    });

    // Show empty slots
    var totalSlots = (state.Ship.weapon_slots || 0) + (state.Ship.defense_slots || 0) + (state.Ship.utility_slots || 0);
    var usedSlots = state.Ship.modules.length;
    if (usedSlots < totalSlots) {
      var emptyTag = document.createElement('span');
      emptyTag.className = 'module-slot-empty';
      emptyTag.textContent = (totalSlots - usedSlots) + ' empty slot' + ((totalSlots - usedSlots) !== 1 ? 's' : '');
      modulesList.appendChild(emptyTag);
    }
  }

  function renderPlayerStats(state, agent) {
    var html = '';

    html += statItem('Username', state ? (state.Username || '--') : '--');
    html += statItem('Credits', state ? formatCredits(state.Credits) + ' cr' : '--', 'credits');
    html += statItem('Faction', agent.faction || '--');
    html += statItem('Empire', state && state.Player ? (state.Player.empire || '--') : '--');

    if (state && state.Player && state.Player.stats) {
      var s = state.Player.stats;
      html += statItem('Ore Mined', formatNum(s.ore_mined));
      html += statItem('Trades', s.trades_completed || 0);
      html += statItem('Systems Found', s.systems_discovered || 0);
      html += statItem('Ships Killed', s.ships_destroyed || 0);
      html += statItem('Deaths', s.times_destroyed || 0);
      html += statItem('Credits Earned', formatNum(s.credits_earned));
    }

    // Skills
    if (state && state.Player && state.Player.skills) {
      var skills = state.Player.skills;
      var skillNames = Object.keys(skills);
      if (skillNames.length > 0) {
        html += '<div class="stat-item full-width"><span class="stat-label">Skills</span>';
        html += '<span class="stat-value">';
        html += skillNames.map(function (k) {
          return k + ': L' + skills[k].level;
        }).join(', ');
        html += '</span></div>';
      }
    }

    playerStats.innerHTML = html;
  }

  function renderLocation(state) {
    var html = '';

    html += statItem('System', state ? (state.CurrentSystem || state.System.Name || '--') : '--', 'highlight');
    html += statItem('POI', state ? (state.CurrentPOI || '--') : '--');
    html += statItem('Docked', state ? (state.Doc ? 'Yes' : 'No') : '--');

    if (state && state.Traveling && state.TravelProgress) {
      var tp = state.TravelProgress;
      html += statItem('Destination', tp.travel_destination || tp.Destination || '--', 'highlight');
      html += statItem('Progress', Math.round((tp.travel_progress || tp.Progress || 0) * 100) + '%');
      html += statItem('Type', tp.travel_type || tp.Type || '--');
    }

    // Nearby players
    if (state && state.Nearby && state.Nearby.length > 0) {
      html += '<div class="stat-item full-width"><span class="stat-label">Nearby (' + state.Nearby.length + ')</span>';
      html += '<span class="stat-value">';
      html += state.Nearby.map(function (p) {
        return (p.username || p.player_id) + ' (' + (p.ship_class || '?') + ')';
      }).join(', ');
      html += '</span></div>';
    }

    if (state && state.InCombat) {
      html += '<div class="stat-item full-width"><span class="stat-label" style="color: var(--accent-red);">IN COMBAT</span></div>';
    }

    locationInfo.innerHTML = html;
  }

  function renderCargo(state) {
    cargoList.innerHTML = '';

    if (!state || !state.Ship) {
      cargoList.innerHTML = '<div class="cargo-empty">No data</div>';
      return;
    }

    var ship = state.Ship;

    // Capacity bar
    var capPct = ship.cargo_capacity > 0 ? ship.cargo_used / ship.cargo_capacity : 0;
    var capHtml = '<div class="stat-item full-width">' +
      '<span class="stat-label">Capacity: ' + Math.round(ship.cargo_used) + ' / ' + Math.round(ship.cargo_capacity) + '</span>' +
      '<div class="stat-bar"><div class="stat-bar-fill" style="width:' + (capPct * 100) + '%; background:#a855f7;"></div></div>' +
      '</div>';
    cargoList.innerHTML = capHtml;

    // Cargo items
    var items = ship.cargo || [];
    if (items.length === 0) {
      cargoList.innerHTML += '<div class="cargo-empty">Cargo hold empty</div>';
    } else {
      items.forEach(function (item) {
        var div = document.createElement('div');
        div.className = 'cargo-item';
        div.innerHTML = '<span class="cargo-item-name">' + escapeHtml(item.item_id) + '</span>' +
          '<span class="cargo-item-qty">' + Math.round(item.quantity) + '</span>';
        cargoList.appendChild(div);
      });
    }
  }

  function renderActionLog(history) {
    actionLog.innerHTML = '';

    if (!history || history.length === 0) {
      actionLog.innerHTML = '<div class="cargo-empty">No action history</div>';
      return;
    }

    history.forEach(function (entry) {
      var div = document.createElement('div');
      div.className = 'log-entry';
      if (entry.success === false) div.classList.add('failed');

      var action = entry.action || entry.Action || '--';
      var target = entry.target || entry.Target || '';
      var result = '';
      if (entry.success !== undefined) {
        result = entry.success ? 'OK' : 'FAIL';
      }
      if (entry.message) {
        target = target ? target + ' - ' + entry.message : entry.message;
      }

      div.innerHTML = '<span class="log-action">' + escapeHtml(action) + '</span>' +
        '<span class="log-target">' + escapeHtml(target) + '</span>' +
        '<span class="log-result">' + escapeHtml(result) + '</span>';
      actionLog.appendChild(div);
    });
  }

  // --- Utilities ---
  function statItem(label, value, extraClass) {
    var cls = 'stat-value' + (extraClass ? ' ' + extraClass : '');
    return '<div class="stat-item"><span class="stat-label">' + escapeHtml(label) + '</span>' +
      '<span class="' + cls + '">' + escapeHtml(String(value)) + '</span></div>';
  }

  function formatCredits(n) {
    if (n === undefined || n === null) return '--';
    return Math.round(n).toLocaleString();
  }

  function formatNum(n) {
    if (n === undefined || n === null) return '--';
    if (n >= 1000000) return (n / 1000000).toFixed(1) + 'M';
    if (n >= 1000) return (n / 1000).toFixed(1) + 'K';
    return Math.round(n).toString();
  }

  function escapeHtml(str) {
    if (!str) return '';
    var div = document.createElement('div');
    div.textContent = str;
    return div.innerHTML;
  }

  // --- Bootstrap ---
  init();
})();
