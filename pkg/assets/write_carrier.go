package assets

import (
	"context"
	"fmt"
	"time"
)

// UpsertCarrier writes the agent's freight carrier standing.
func (s *Store) UpsertCarrier(ctx context.Context, playerID string, c Carrier, now time.Time) error {
	if s == nil || s.db == nil || playerID == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agent_carrier (player_id, tier, successful_deliveries, delivered_value,
			priority_deliveries, returns, breaches, defaults, active_contracts,
			active_liability, outstanding_debt, debt_blocks_acceptance, next_tier,
			at_maximum_tier, required_successful_deliveries, remaining_successful_deliveries,
			required_delivered_value, remaining_delivered_value, active_contract_limit,
			active_contracts_unlimited, aggregate_liability_limit,
			remaining_aggregate_liability, single_package_liability_limit,
			liability_unlimited, captured_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(player_id) DO UPDATE SET
			tier=excluded.tier, successful_deliveries=excluded.successful_deliveries,
			delivered_value=excluded.delivered_value,
			priority_deliveries=excluded.priority_deliveries, returns=excluded.returns,
			breaches=excluded.breaches, defaults=excluded.defaults,
			active_contracts=excluded.active_contracts,
			active_liability=excluded.active_liability,
			outstanding_debt=excluded.outstanding_debt,
			debt_blocks_acceptance=excluded.debt_blocks_acceptance,
			next_tier=excluded.next_tier, at_maximum_tier=excluded.at_maximum_tier,
			required_successful_deliveries=excluded.required_successful_deliveries,
			remaining_successful_deliveries=excluded.remaining_successful_deliveries,
			required_delivered_value=excluded.required_delivered_value,
			remaining_delivered_value=excluded.remaining_delivered_value,
			active_contract_limit=excluded.active_contract_limit,
			active_contracts_unlimited=excluded.active_contracts_unlimited,
			aggregate_liability_limit=excluded.aggregate_liability_limit,
			remaining_aggregate_liability=excluded.remaining_aggregate_liability,
			single_package_liability_limit=excluded.single_package_liability_limit,
			liability_unlimited=excluded.liability_unlimited,
			captured_at=excluded.captured_at`,
		playerID, c.Tier, c.SuccessfulDeliveries, c.DeliveredValue,
		c.PriorityDeliveries, c.Returns, c.Breaches, c.Defaults, c.ActiveContracts,
		c.ActiveLiability, c.OutstandingDebt, c.DebtBlocksAcceptance, c.NextTier,
		c.AtMaximumTier, c.RequiredSuccessfulDeliveries, c.RemainingSuccessfulDeliveries,
		c.RequiredDeliveredValue, c.RemainingDeliveredValue, c.ActiveContractLimit,
		c.ActiveContractsUnlimited, c.AggregateLiabilityLimit,
		c.RemainingAggregateLiability, c.SinglePackageLiabilityLimit,
		c.LiabilityUnlimited, rfc3339(now))
	if err != nil {
		return fmt.Errorf("assets: upsert carrier %s: %w", playerID, err)
	}

	return nil
}
