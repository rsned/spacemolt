package respfmt

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// orderResult is one row of the action_result.results[] array returned by
// create_buy_order / create_sell_order in bulk mode. Failure rows may
// carry Error/ErrorCode instead of Message and may omit Item/ItemID
// entirely (e.g. contraband_restricted), so the renderer has to fall
// back gracefully.
type orderResult struct {
	Index             int    `json:"index"`
	Item              string `json:"item,omitempty"`
	ItemID            string `json:"item_id,omitempty"`
	Success           bool   `json:"success"`
	Message           string `json:"message,omitempty"`
	Error             string `json:"error,omitempty"`
	ErrorCode         string `json:"error_code,omitempty"`
	OrderID           string `json:"order_id,omitempty"`
	PriceEach         int64  `json:"price_each,omitempty"`
	Quantity          int64  `json:"quantity,omitempty"`
	ListingFee        int64  `json:"listing_fee,omitempty"`
	TotalEscrowed     int64  `json:"total_escrowed,omitempty"`
	RemainingEscrowed int64  `json:"remaining_escrowed,omitempty"`
}

// failureMessage picks the most informative human-readable string off a
// failure row, prefixing the error_code when present.
func (r orderResult) failureMessage() string {
	msg := r.Message
	if msg == "" {
		msg = r.Error
	}
	if r.ErrorCode != "" {
		if msg == "" {
			return r.ErrorCode
		}
		return r.ErrorCode + ": " + msg
	}
	return msg
}

// displayID returns item_id if set, otherwise a stable fallback derived
// from the input index — failure rows for some error codes ship without
// item_id at all.
func (r orderResult) displayID() string {
	if r.ItemID != "" {
		return r.ItemID
	}
	return fmt.Sprintf("(index %d)", r.Index)
}

// displayName returns item if set, otherwise displayID() so the column
// is never blank.
func (r orderResult) displayName() string {
	if r.Item != "" {
		return r.Item
	}
	return r.displayID()
}

type orderSummary struct {
	Total     int `json:"total"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
}

type bulkOrderResult struct {
	Action  string        `json:"action"`
	Mode    string        `json:"mode"`
	Results []orderResult `json:"results"`
	Summary orderSummary  `json:"summary"`
}

// CreateBuyOrder renders the action_result body of a create_buy_order
// command, listing failures explicitly. Returns "" if raw can't be parsed
// as the bulk-mode shape.
//
// The accepted shapes are either the bare result body or the full
// action_result frame ({"command":..., "result":{...}}).
func CreateBuyOrder(raw []byte) string {
	return formatCreateOrder(raw, "create_buy_order")
}

// CreateSellOrder mirrors CreateBuyOrder for sell-side orders.
func CreateSellOrder(raw []byte) string {
	return formatCreateOrder(raw, "create_sell_order")
}

func formatCreateOrder(raw []byte, label string) string {
	body := unwrapResult(raw)
	var br bulkOrderResult
	if err := json.Unmarshal(body, &br); err != nil {
		return ""
	}
	if len(br.Results) == 0 && br.Summary.Total == 0 {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s: %d/%d succeeded", label, br.Summary.Succeeded, br.Summary.Total)
	if br.Summary.Failed > 0 {
		fmt.Fprintf(&b, ", %d failed", br.Summary.Failed)
	}
	b.WriteByte('\n')

	if br.Summary.Failed == 0 {
		return b.String()
	}

	failures := make([]orderResult, 0, br.Summary.Failed)
	for _, r := range br.Results {
		if !r.Success {
			failures = append(failures, r)
		}
	}
	// Sort by input index for predictable, batch-aligned output.
	sort.Slice(failures, func(i, j int) bool {
		return failures[i].Index < failures[j].Index
	})

	idW, nameW := len("ID"), len("Name")
	for _, r := range failures {
		idW = max(idW, len(r.displayID()))
		nameW = max(nameW, len(r.displayName()))
	}

	fmt.Fprintf(&b, "  Failures:\n")
	for _, r := range failures {
		fmt.Fprintf(&b, "    ✗ %-*s | %-*s | %s\n",
			idW, r.displayID(), nameW, r.displayName(), r.failureMessage())
	}
	return b.String()
}

// unwrapResult returns the inner "result" body if raw is an action_result
// frame ({"command":..., "result":{...}, "tick":N}); otherwise returns
// raw unchanged.
func unwrapResult(raw []byte) []byte {
	var probe struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return raw
	}
	if len(probe.Result) == 0 {
		return raw
	}
	return probe.Result
}
