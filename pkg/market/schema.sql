-- Item catalog
CREATE TABLE IF NOT EXISTS items (
    item_id         TEXT PRIMARY KEY,
    item_name       TEXT NOT NULL,
    category        TEXT,
    first_seen_utc  TEXT NOT NULL,
    last_updated_utc TEXT NOT NULL
);

-- Stations (points of interest with markets)
CREATE TABLE IF NOT EXISTS stations (
    station_id      TEXT PRIMARY KEY,
    station_name    TEXT NOT NULL,
    system_id       TEXT NOT NULL,
    system_name     TEXT NOT NULL,
    first_seen_utc  TEXT NOT NULL,
    last_updated_utc TEXT NOT NULL
);

-- Individual orders (main fact table)
CREATE TABLE IF NOT EXISTS market_orders (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    station_id      TEXT NOT NULL,
    item_id         TEXT NOT NULL,
    side            TEXT NOT NULL CHECK (side IN ('buy', 'sell')),
    price_each      REAL NOT NULL,
    quantity        REAL NOT NULL,
    my_quantity     REAL DEFAULT 0,
    source          TEXT,
    captured_at     TEXT NOT NULL,
    bucket_utc      TEXT NOT NULL,
    FOREIGN KEY (station_id) REFERENCES stations(station_id),
    FOREIGN KEY (item_id) REFERENCES items(item_id)
);

CREATE INDEX IF NOT EXISTS idx_orders_station_item ON market_orders(station_id, item_id, bucket_utc);
CREATE INDEX IF NOT EXISTS idx_orders_item_time ON market_orders(item_id, captured_at);
-- Partial buy-side index: makes the "latest buy orders per (station, item)"
-- scan (demand report) index-driven instead of a full-table walk (97s -> ~4s
-- on a 40M-row live DB).
CREATE INDEX IF NOT EXISTS idx_orders_buy_station_item_cap ON market_orders(station_id, item_id, captured_at) WHERE side='buy';
CREATE INDEX IF NOT EXISTS idx_orders_bucket ON market_orders(bucket_utc);

-- Hourly OHLCV aggregates
CREATE TABLE IF NOT EXISTS market_ohlcv (
    station_id      TEXT NOT NULL,
    item_id         TEXT NOT NULL,
    side            TEXT NOT NULL CHECK (side IN ('buy', 'sell')),
    bucket_utc      TEXT NOT NULL,
    open_price      REAL NOT NULL,
    high_price      REAL NOT NULL,
    low_price       REAL NOT NULL,
    close_price     REAL NOT NULL,
    volume          REAL NOT NULL,
    trade_count     INTEGER NOT NULL,
    vwap            REAL NOT NULL,
    FOREIGN KEY (station_id) REFERENCES stations(station_id),
    FOREIGN KEY (item_id) REFERENCES items(item_id),
    PRIMARY KEY (station_id, item_id, side, bucket_utc)
);

-- Arbitrage opportunities
CREATE TABLE IF NOT EXISTS arbitrage_opportunities (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    from_station_id     TEXT NOT NULL,
    to_station_id       TEXT NOT NULL,
    item_id             TEXT NOT NULL,
    action_type         TEXT NOT NULL CHECK (action_type IN ('buy_then_sell', 'sell_then_buy')),
    buy_price           REAL NOT NULL,
    sell_price          REAL NOT NULL,
    quantity            REAL NOT NULL,
    source_units        REAL NOT NULL DEFAULT 0,
    gross_profit        REAL NOT NULL,
    fuel_cost           REAL NOT NULL,
    travel_ticks        INTEGER NOT NULL,
    cargo_required      REAL NOT NULL,
    risk_score          REAL DEFAULT 0,
    claimed_by          TEXT,
    claimed_at          TEXT,
    completed_at        TEXT,
    cycles_seen         INTEGER DEFAULT 1,
    status              TEXT DEFAULT 'available' CHECK (status IN ('available', 'claimed', 'completed', 'expired')),
    expires_at          TEXT NOT NULL,
    discovered_at       TEXT NOT NULL,
    discovered_by       TEXT NOT NULL,
    notes               TEXT,
    FOREIGN KEY (from_station_id) REFERENCES stations(station_id),
    FOREIGN KEY (to_station_id) REFERENCES stations(station_id),
    FOREIGN KEY (item_id) REFERENCES items(item_id)
);

CREATE INDEX IF NOT EXISTS idx_arbitrage_status ON arbitrage_opportunities(status, expires_at);
CREATE INDEX IF NOT EXISTS idx_arbitrage_item ON arbitrage_opportunities(item_id, status);

-- LLM market analysis (analyze_market output)
CREATE TABLE IF NOT EXISTS analyses (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    station_id        TEXT NOT NULL,
    station_name      TEXT,
    system_id         TEXT,
    system_name       TEXT,
    game_tick         INTEGER NOT NULL,
    captured_at       TEXT NOT NULL,
    agent_id          TEXT,
    mode              TEXT,
    skill_level       INTEGER,
    scanning_range    TEXT,
    stations_in_range INTEGER,
    items_scanned     INTEGER,
    top_insights      TEXT,
    total_items       INTEGER,
    total_pages       INTEGER,
    page              INTEGER,
    hint              TEXT,
    xp_gained         TEXT,
    analysis_data     TEXT
);

CREATE INDEX IF NOT EXISTS idx_analyses_station_time ON analyses(station_id, captured_at);

-- Per-haul real outcomes + per-leg timing (dashboard Component A).
CREATE TABLE IF NOT EXISTS haul_results (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    opp_id          INTEGER NOT NULL,
    agent_id        TEXT NOT NULL,
    item_id         TEXT NOT NULL,
    qty             REAL NOT NULL,
    buy_price_paid  REAL NOT NULL,
    sell_price_got  REAL NOT NULL,
    realized_profit REAL NOT NULL,
    jumps_traveled  INTEGER NOT NULL,
    claimed_at      TEXT NOT NULL,
    arrived_src_at  TEXT NOT NULL,
    bought_at       TEXT NOT NULL,
    arrived_dst_at  TEXT NOT NULL,
    sold_at         TEXT NOT NULL,
    claimed_tick    INTEGER NOT NULL,
    arrived_src_tick INTEGER NOT NULL,
    bought_tick     INTEGER NOT NULL,
    arrived_dst_tick INTEGER NOT NULL,
    sold_tick       INTEGER NOT NULL,
    created_at      TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_haul_results_agent_time ON haul_results(agent_id, sold_at);

-- Periodic per-hauler balance/fuel/cargo snapshots (dashboard Component B).
CREATE TABLE IF NOT EXISTS fleet_timeseries (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    ts             TEXT NOT NULL,
    agent_id       TEXT NOT NULL,
    role           TEXT,
    system         TEXT,
    docked         INTEGER,
    credits        REAL,
    fuel           REAL,
    max_fuel       REAL,
    cargo_used     REAL,
    cargo_capacity REAL
);

CREATE INDEX IF NOT EXISTS idx_fleet_timeseries_agent_time ON fleet_timeseries(agent_id, ts);

-- Per-mission real outcomes for the mission-runner fleet (spec 2026-07-16).
CREATE TABLE IF NOT EXISTS mission_results (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    agent_id        TEXT NOT NULL,
    mission_id      TEXT NOT NULL,
    template_id     TEXT,
    mission_type    TEXT,
    title           TEXT,
    from_base_id    TEXT,
    to_base_id      TEXT,
    item_id         TEXT,
    qty             REAL,
    expected_reward REAL NOT NULL,
    credits_earned  REAL NOT NULL,
    item_cost       REAL NOT NULL,
    fuel_cost       REAL NOT NULL,
    jumps           INTEGER NOT NULL,
    outcome         TEXT NOT NULL,
    reason          TEXT DEFAULT '',
    accepted_at     TEXT NOT NULL,
    finished_at     TEXT NOT NULL,
    accepted_tick   INTEGER,
    finished_tick   INTEGER,
    created_at      TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_mission_results_agent_time ON mission_results(agent_id, finished_at);

-- One row per station, upserted in place (replace-on-capture — never grows).
-- fuel_price_all_in is the per-unit cost a hauler pays; captured_at is a
-- freshness stamp on the single row (not history).
CREATE TABLE IF NOT EXISTS station_fuel_prices (
    station_id        TEXT PRIMARY KEY,
    fuel_price        INTEGER NOT NULL,
    fuel_tax_per_unit INTEGER NOT NULL,
    fuel_price_all_in INTEGER NOT NULL,
    captured_at       TEXT NOT NULL,
    captured_by       TEXT
);
