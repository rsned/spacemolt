// ship-graphic.js - SVG ship schematic renderer inspired by Battletech Aerotech 2
// Renders a top-down ship diagram with hull sections, shields, and system bars

const ShipGraphic = (function () {

  // Color thresholds: percentage -> color
  function barColor(pct) {
    if (pct > 0.75) return '#22c55e'; // green
    if (pct > 0.50) return '#eab308'; // yellow
    if (pct > 0.25) return '#f97316'; // orange
    return '#ef4444'; // red
  }

  function sectionColor(pct) {
    if (pct > 0.75) return 'rgba(34, 197, 94, 0.25)';
    if (pct > 0.50) return 'rgba(234, 179, 8, 0.25)';
    if (pct > 0.25) return 'rgba(249, 115, 22, 0.25)';
    return 'rgba(239, 68, 68, 0.3)';
  }

  function sectionStroke(pct) {
    if (pct > 0.75) return '#22c55e';
    if (pct > 0.50) return '#eab308';
    if (pct > 0.25) return '#f97316';
    return '#ef4444';
  }

  function formatNum(n) {
    if (n === undefined || n === null) return '--';
    if (n >= 1000000) return (n / 1000000).toFixed(1) + 'M';
    if (n >= 1000) return (n / 1000).toFixed(1) + 'K';
    return Math.round(n).toString();
  }

  // Build a horizontal bar gauge SVG snippet
  function gaugeBar(x, y, width, height, pct, label, valueText, color) {
    if (color === undefined) color = barColor(pct);
    var fillW = Math.max(0, Math.min(1, pct)) * width;
    return '' +
      '<text x="' + x + '" y="' + (y - 4) + '" class="gauge-label">' + label + '</text>' +
      '<text x="' + (x + width) + '" y="' + (y - 4) + '" class="gauge-value" text-anchor="end">' + valueText + '</text>' +
      '<rect x="' + x + '" y="' + y + '" width="' + width + '" height="' + height + '" rx="2" fill="#1e293b" />' +
      '<rect x="' + x + '" y="' + y + '" width="' + fillW + '" height="' + height + '" rx="2" fill="' + color + '" />';
  }

  // Main render function
  function render(container, ship, state) {
    if (!ship) {
      container.innerHTML = '<div class="empty-text">No ship data available</div>';
      return;
    }

    var hullPct = ship.max_hull > 0 ? ship.hull / ship.max_hull : 0;
    var shieldPct = ship.max_shield > 0 ? ship.shield / ship.max_shield : 0;
    var fuelPct = ship.max_fuel > 0 ? ship.fuel / ship.max_fuel : 0;
    var cargoPct = ship.cargo_capacity > 0 ? ship.cargo_used / ship.cargo_capacity : 0;
    var cpuPct = ship.cpu_capacity > 0 ? ship.cpu_used / ship.cpu_capacity : 0;
    var powerPct = ship.power_capacity > 0 ? ship.power_used / ship.power_capacity : 0;

    var svgW = 440;
    var svgH = 520;

    // Ship body center
    var cx = 160;
    var cy = 200;

    var svg = '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ' + svgW + ' ' + svgH + '" width="' + svgW + '" height="' + svgH + '">';

    // Defs: styles
    svg += '<defs>';
    svg += '<style>';
    svg += '.ship-section { stroke-width: 1.5; transition: fill 0.5s; }';
    svg += '.section-label { font-family: Consolas, "Courier New", monospace; font-size: 10px; fill: #94a3b8; text-anchor: middle; }';
    svg += '.section-value { font-family: Consolas, "Courier New", monospace; font-size: 11px; fill: #e2e8f0; text-anchor: middle; font-weight: bold; }';
    svg += '.gauge-label { font-family: Consolas, "Courier New", monospace; font-size: 10px; fill: #64748b; text-transform: uppercase; letter-spacing: 1px; }';
    svg += '.gauge-value { font-family: Consolas, "Courier New", monospace; font-size: 10px; fill: #94a3b8; }';
    svg += '.ship-name { font-family: Consolas, "Courier New", monospace; font-size: 13px; fill: #22d3ee; text-anchor: middle; font-weight: bold; letter-spacing: 2px; }';
    svg += '.ship-class { font-family: Consolas, "Courier New", monospace; font-size: 10px; fill: #64748b; text-anchor: middle; text-transform: uppercase; letter-spacing: 1px; }';
    svg += '.armor-label { font-family: Consolas, "Courier New", monospace; font-size: 9px; fill: #64748b; text-anchor: middle; }';
    svg += '.armor-value { font-family: Consolas, "Courier New", monospace; font-size: 12px; fill: #f97316; text-anchor: middle; font-weight: bold; }';
    svg += '.slot-label { font-family: Consolas, "Courier New", monospace; font-size: 9px; fill: #64748b; }';
    svg += '.slot-value { font-family: Consolas, "Courier New", monospace; font-size: 11px; fill: #94a3b8; }';
    svg += '</style>';
    // Glow filter for shield
    svg += '<filter id="shield-glow" x="-20%" y="-20%" width="140%" height="140%">';
    svg += '<feGaussianBlur stdDeviation="4" result="blur"/>';
    svg += '<feMerge><feMergeNode in="blur"/><feMergeNode in="SourceGraphic"/></feMerge>';
    svg += '</filter>';
    svg += '</defs>';

    // Background grid (subtle)
    svg += '<rect width="' + svgW + '" height="' + svgH + '" fill="#0a0e17" />';
    for (var gx = 0; gx < svgW; gx += 20) {
      svg += '<line x1="' + gx + '" y1="0" x2="' + gx + '" y2="' + svgH + '" stroke="#1a2332" stroke-width="0.5"/>';
    }
    for (var gy = 0; gy < svgH; gy += 20) {
      svg += '<line x1="0" y1="' + gy + '" x2="' + svgW + '" y2="' + gy + '" stroke="#1a2332" stroke-width="0.5"/>';
    }

    // === SHIP SILHOUETTE (top-down fighter/freighter shape) ===
    // Shield aura (outer glow)
    if (shieldPct > 0) {
      var shieldAlpha = 0.1 + shieldPct * 0.2;
      svg += '<path d="' +
        'M ' + cx + ' ' + (cy - 120) + ' ' +
        'C ' + (cx + 60) + ' ' + (cy - 100) + ' ' + (cx + 100) + ' ' + (cy - 40) + ' ' + (cx + 110) + ' ' + (cy + 20) + ' ' +
        'L ' + (cx + 80) + ' ' + (cy + 100) + ' ' +
        'L ' + (cx - 80) + ' ' + (cy + 100) + ' ' +
        'L ' + (cx - 110) + ' ' + (cy + 20) + ' ' +
        'C ' + (cx - 100) + ' ' + (cy - 40) + ' ' + (cx - 60) + ' ' + (cy - 100) + ' ' + cx + ' ' + (cy - 120) + ' Z' +
        '" fill="rgba(59, 130, 246, ' + shieldAlpha + ')" stroke="rgba(59, 130, 246, 0.4)" stroke-width="1" filter="url(#shield-glow)" />';
    }

    // --- NOSE section ---
    var noseFill = sectionColor(hullPct);
    var noseStroke = sectionStroke(hullPct);
    svg += '<path class="ship-section" d="' +
      'M ' + cx + ' ' + (cy - 100) + ' ' +
      'L ' + (cx + 30) + ' ' + (cy - 50) + ' ' +
      'L ' + (cx - 30) + ' ' + (cy - 50) + ' Z' +
      '" fill="' + noseFill + '" stroke="' + noseStroke + '" />';
    svg += '<text x="' + cx + '" y="' + (cy - 62) + '" class="section-label">NOSE</text>';

    // --- FUSELAGE (center body) ---
    svg += '<path class="ship-section" d="' +
      'M ' + (cx - 30) + ' ' + (cy - 50) + ' ' +
      'L ' + (cx + 30) + ' ' + (cy - 50) + ' ' +
      'L ' + (cx + 35) + ' ' + (cy + 40) + ' ' +
      'L ' + (cx - 35) + ' ' + (cy + 40) + ' Z' +
      '" fill="' + noseFill + '" stroke="' + noseStroke + '" />';

    // Hull integrity marker in center
    svg += '<text x="' + cx + '" y="' + (cy - 10) + '" class="section-label">HULL</text>';
    svg += '<text x="' + cx + '" y="' + (cy + 8) + '" class="section-value">' + Math.round(ship.hull) + '/' + Math.round(ship.max_hull) + '</text>';

    // --- LEFT WING ---
    svg += '<path class="ship-section" d="' +
      'M ' + (cx - 30) + ' ' + (cy - 30) + ' ' +
      'L ' + (cx - 90) + ' ' + (cy + 10) + ' ' +
      'L ' + (cx - 85) + ' ' + (cy + 60) + ' ' +
      'L ' + (cx - 35) + ' ' + (cy + 40) + ' Z' +
      '" fill="' + noseFill + '" stroke="' + noseStroke + '" />';

    // --- RIGHT WING ---
    svg += '<path class="ship-section" d="' +
      'M ' + (cx + 30) + ' ' + (cy - 30) + ' ' +
      'L ' + (cx + 90) + ' ' + (cy + 10) + ' ' +
      'L ' + (cx + 85) + ' ' + (cy + 60) + ' ' +
      'L ' + (cx + 35) + ' ' + (cy + 40) + ' Z' +
      '" fill="' + noseFill + '" stroke="' + noseStroke + '" />';

    // --- AFT (engine section) ---
    svg += '<path class="ship-section" d="' +
      'M ' + (cx - 35) + ' ' + (cy + 40) + ' ' +
      'L ' + (cx + 35) + ' ' + (cy + 40) + ' ' +
      'L ' + (cx + 40) + ' ' + (cy + 80) + ' ' +
      'L ' + (cx - 40) + ' ' + (cy + 80) + ' Z' +
      '" fill="' + noseFill + '" stroke="' + noseStroke + '" />';
    svg += '<text x="' + cx + '" y="' + (cy + 65) + '" class="section-label">AFT</text>';

    // Engine glow
    svg += '<ellipse cx="' + (cx - 15) + '" cy="' + (cy + 82) + '" rx="8" ry="4" fill="rgba(34, 211, 238, 0.6)" />';
    svg += '<ellipse cx="' + (cx + 15) + '" cy="' + (cy + 82) + '" rx="8" ry="4" fill="rgba(34, 211, 238, 0.6)" />';

    // Wing labels
    svg += '<text x="' + (cx - 63) + '" y="' + (cy + 22) + '" class="section-label">L.WING</text>';
    svg += '<text x="' + (cx + 63) + '" y="' + (cy + 22) + '" class="section-label">R.WING</text>';

    // --- ARMOR indicator ---
    svg += '<text x="' + cx + '" y="' + (cy + 110) + '" class="armor-label">ARMOR RATING</text>';
    svg += '<text x="' + cx + '" y="' + (cy + 126) + '" class="armor-value">' + (ship.armor || 0) + '</text>';

    // --- SPEED indicator ---
    svg += '<text x="' + (cx - 70) + '" y="' + (cy + 110) + '" class="armor-label">SPEED</text>';
    svg += '<text x="' + (cx - 70) + '" y="' + (cy + 126) + '" class="armor-value">' + (ship.speed || 0) + '</text>';

    // --- SHIELD RECHARGE ---
    svg += '<text x="' + (cx + 70) + '" y="' + (cy + 110) + '" class="armor-label">SHLD RCHG</text>';
    svg += '<text x="' + (cx + 70) + '" y="' + (cy + 126) + '" class="armor-value">' + (ship.shield_recharge || 0) + '/tick</text>';

    // Ship name & class at top
    svg += '<text x="' + cx + '" y="' + 22 + '" class="ship-name">' + escapeXml(ship.name || 'UNKNOWN') + '</text>';
    svg += '<text x="' + cx + '" y="' + 38 + '" class="ship-class">' + escapeXml(ship.class_id || '') + '</text>';

    // === SYSTEM GAUGES (right side) ===
    var gaugeX = 250;
    var gaugeW = 170;
    var gaugeH = 10;
    var gaugeY = 22;
    var gaugeSpacing = 38;

    // Shield
    svg += gaugeBar(gaugeX, gaugeY, gaugeW, gaugeH,
      shieldPct, 'SHIELD', Math.round(ship.shield) + '/' + Math.round(ship.max_shield),
      '#3b82f6');
    gaugeY += gaugeSpacing;

    // Hull
    svg += gaugeBar(gaugeX, gaugeY, gaugeW, gaugeH,
      hullPct, 'HULL', Math.round(ship.hull) + '/' + Math.round(ship.max_hull));
    gaugeY += gaugeSpacing;

    // Fuel
    svg += gaugeBar(gaugeX, gaugeY, gaugeW, gaugeH,
      fuelPct, 'FUEL', Math.round(ship.fuel) + '/' + Math.round(ship.max_fuel),
      '#22d3ee');
    gaugeY += gaugeSpacing;

    // Cargo
    svg += gaugeBar(gaugeX, gaugeY, gaugeW, gaugeH,
      cargoPct, 'CARGO', Math.round(ship.cargo_used) + '/' + Math.round(ship.cargo_capacity),
      '#a855f7');
    gaugeY += gaugeSpacing;

    // CPU
    svg += gaugeBar(gaugeX, gaugeY, gaugeW, gaugeH,
      cpuPct, 'CPU', Math.round(ship.cpu_used) + '/' + Math.round(ship.cpu_capacity),
      '#f97316');
    gaugeY += gaugeSpacing;

    // Power
    svg += gaugeBar(gaugeX, gaugeY, gaugeW, gaugeH,
      powerPct, 'POWER', Math.round(ship.power_used) + '/' + Math.round(ship.power_capacity),
      '#eab308');
    gaugeY += gaugeSpacing;

    // === SLOT INFO (below gauges) ===
    gaugeY += 10;
    svg += '<text x="' + gaugeX + '" y="' + gaugeY + '" class="slot-label">WEAPON SLOTS</text>';
    svg += '<text x="' + (gaugeX + gaugeW) + '" y="' + gaugeY + '" class="slot-value" text-anchor="end">' + (ship.weapon_slots || 0) + '</text>';
    gaugeY += 18;

    svg += '<text x="' + gaugeX + '" y="' + gaugeY + '" class="slot-label">DEFENSE SLOTS</text>';
    svg += '<text x="' + (gaugeX + gaugeW) + '" y="' + gaugeY + '" class="slot-value" text-anchor="end">' + (ship.defense_slots || 0) + '</text>';
    gaugeY += 18;

    svg += '<text x="' + gaugeX + '" y="' + gaugeY + '" class="slot-label">UTILITY SLOTS</text>';
    svg += '<text x="' + (gaugeX + gaugeW) + '" y="' + gaugeY + '" class="slot-value" text-anchor="end">' + (ship.utility_slots || 0) + '</text>';
    gaugeY += 18;

    svg += '<text x="' + gaugeX + '" y="' + gaugeY + '" class="slot-label">MODULES INSTALLED</text>';
    var moduleCount = (ship.modules && ship.modules.length) || 0;
    svg += '<text x="' + (gaugeX + gaugeW) + '" y="' + gaugeY + '" class="slot-value" text-anchor="end">' + moduleCount + '</text>';

    // === CREDITS (bottom area) ===
    if (state && state.Credits !== undefined) {
      svg += '<text x="' + (svgW / 2) + '" y="' + (svgH - 40) + '" style="font-family: Consolas, monospace; font-size: 10px; fill: #64748b; text-anchor: middle; text-transform: uppercase; letter-spacing: 1px;">CREDITS</text>';
      svg += '<text x="' + (svgW / 2) + '" y="' + (svgH - 20) + '" style="font-family: Consolas, monospace; font-size: 18px; fill: #eab308; text-anchor: middle; font-weight: bold;">' + formatCredits(state.Credits) + ' cr</text>';
    }

    svg += '</svg>';
    container.innerHTML = svg;
  }

  function formatCredits(n) {
    if (n === undefined || n === null) return '--';
    return Math.round(n).toLocaleString();
  }

  function escapeXml(str) {
    if (!str) return '';
    return str.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
  }

  return {
    render: render,
    barColor: barColor,
  };
})();
