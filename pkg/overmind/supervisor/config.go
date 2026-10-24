package supervisor

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// WorkerSpec is one roster entry the supervisor will spawn.
type WorkerSpec struct {
	AgentID string `yaml:"agent_id"`
	Role    string `yaml:"role"`
	Station string `yaml:"station"`
	// MissionCategories, when set, is forwarded to the worker as
	// --mission-categories (comma-joined) so a mission-runner fleet can run
	// beyond the delivery default — e.g. the learning pool's
	// [delivery, exploration]. Empty leaves the worker's delivery-only default.
	MissionCategories []string `yaml:"mission_categories,omitempty"`
	// EnableFreight forwards --enable-freight, opting this worker into the
	// /shipping carrier path (evaluated co-equally with the mission board).
	// Default false = freight fully dormant. Canary one worker before any
	// pool rollout — see the Rollout section of
	// docs/superpowers/plans/2026-07-20-shipping-carrier-subproject-b.md.
	EnableFreight bool `yaml:"enable_freight,omitempty"`
	// FreightMaxPackages forwards --freight-max-packages, capping concurrent
	// freight contracts (sub-project C multi-package trips). 0/1 = the v1
	// single-contract behavior; canary fighter-4 runs 3. Layers UNDER the
	// server/cargo headroom gates.
	FreightMaxPackages int `yaml:"freight_max_packages,omitempty"`
	// Consign forwards --consign, a standing instruction to hand named items
	// from this station's personal storage to a named hauler as part of the
	// ordinary idle pass -- no manual control and no interrupted routine.
	//
	// Each entry is item:recipient:min[:keep]. It is an explicit ALLOW-LIST:
	// gifting is irreversible, and the drone project's reserved energy
	// crystals and a ship's fuel cells sit in the same storage, so there are
	// deliberately no patterns. keep holds back a reserve for an item the
	// collector also consumes.
	//
	// Works without a rendezvous: send_gift lands in the recipient's storage
	// at the SENDING station and waits for them, so a pinned collector can
	// consign to a hauler that is days away.
	Consign []string `yaml:"consign,omitempty"`
	// ConsignOffer forwards --consign-offer: items this collector advertises
	// to the whole hauler fleet as consigned (withdraw_then_sell) routes,
	// instead of naming a recipient. Whichever hauler claims the route gets
	// the gift, through the handoff queue. Each entry is item[:keep]; the same
	// allow-list discipline as Consign applies.
	ConsignOffer []string `yaml:"consign_offer,omitempty"`
	// Pipeline forwards --pipeline "item,system,poi,to,recipient": the pinned
	// mine-and-hand-off route of a pipeline_miner worker.
	Pipeline string `yaml:"pipeline,omitempty"`
	// DisableFreightBootstrap forwards --freight-bootstrap=false, turning off the
	// probationary loss-leader floor while leaving freight enabled. Zero value
	// (false) keeps the CLI default (bootstrap on).
	DisableFreightBootstrap bool `yaml:"disable_freight_bootstrap,omitempty"`
}

type fleetFile struct {
	Workers []WorkerSpec `yaml:"workers"`
}

// LoadFleet parses the fleet roster YAML at path.
func LoadFleet(path string) ([]WorkerSpec, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("supervisor: read fleet: %w", err)
	}
	var ff fleetFile
	if err := yaml.Unmarshal(raw, &ff); err != nil {
		return nil, fmt.Errorf("supervisor: parse fleet: %w", err)
	}
	for i, w := range ff.Workers {
		if w.AgentID == "" {
			return nil, fmt.Errorf("supervisor: fleet entry %d missing agent_id", i)
		}
	}
	return ff.Workers, nil
}
