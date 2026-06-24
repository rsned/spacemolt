package main

import (
	"testing"
	"time"
)

func TestParseScanArgsDefaults(t *testing.T) {
	cfg, err := parseScanArgs(nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.dbPath != "data/market.db" {
		t.Errorf("dbPath = %q", cfg.dbPath)
	}
	if cfg.opts.MinProfit != 1000 || cfg.opts.MinPrice != 10 || cfg.opts.MinQuantity != 1 {
		t.Errorf("defaults = profit %v price %v qty %v", cfg.opts.MinProfit, cfg.opts.MinPrice, cfg.opts.MinQuantity)
	}
	if cfg.opts.ExpiresIn != 6*time.Hour {
		t.Errorf("expires = %v", cfg.opts.ExpiresIn)
	}
	if cfg.opts.Limit != 500 {
		t.Errorf("limit = %v", cfg.opts.Limit)
	}
	if cfg.asJSON || len(cfg.opts.Items) != 0 {
		t.Errorf("asJSON=%v items=%v", cfg.asJSON, cfg.opts.Items)
	}
}

func TestParseScanArgsOverrides(t *testing.T) {
	args := []string{
		"-min-profit", "500", "-min-price", "1", "-items", "iron_ore, copper_ore",
		"-expires", "2h", "-limit", "10", "-json", "-market-db-path", "/tmp/x.db",
	}
	cfg, err := parseScanArgs(args)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.opts.MinProfit != 500 || cfg.opts.MinPrice != 1 || cfg.opts.ExpiresIn != 2*time.Hour || cfg.opts.Limit != 10 {
		t.Errorf("overrides wrong: %+v", cfg.opts)
	}
	if cfg.dbPath != "/tmp/x.db" {
		t.Errorf("dbPath = %q", cfg.dbPath)
	}
	if !cfg.asJSON {
		t.Errorf("asJSON should be true")
	}
	if len(cfg.opts.Items) != 2 || cfg.opts.Items[0] != "iron_ore" || cfg.opts.Items[1] != "copper_ore" {
		t.Errorf("items = %v", cfg.opts.Items)
	}
}

func TestParseIDAgentRequiresFlags(t *testing.T) {
	if _, _, _, err := parseIDAgent("claim", nil); err == nil {
		t.Error("expected error when --id/--agent missing")
	}
	_, id, agent, err := parseIDAgent("claim", []string{"-id", "7", "-agent", "a1"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if id != 7 || agent != "a1" {
		t.Errorf("id=%d agent=%q", id, agent)
	}
}
