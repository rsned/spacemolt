package game

import (
	"encoding/json"
	"log"
	"os"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/mattn/go-isatty"
	"github.com/rsned/spacemolt/internal/protocol"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// apiChangeLogger writes to stderr regardless of debug settings so that
// server API changes are always visible in logs. When stderr is a TTY,
// the prefix is rendered in bold bright-yellow on red so it stands out
// against the surrounding agent log noise.
var apiChangeLogger = log.New(log.Writer(), apiChangePrefix(), log.LstdFlags)

func apiChangePrefix() string {
	const plain = "[SERVER API CHANGE] "
	if isatty.IsTerminal(os.Stderr.Fd()) {
		// Bold (1) + bright yellow fg (93) + red bg (41); reset (0).
		return "\x1b[1;93;41m[SERVER API CHANGE]\x1b[0m "
	}
	return plain
}

// loggedAPIChanges deduplicates warnings so each unique change is logged
// only once per process lifetime.
var loggedAPIChanges sync.Map

// commonOKFields are top-level payload keys that can appear in any TypeOK
// response as part of the server envelope. These are parsed by
// parsePlayerData, parseShipData, etc. and are not action-specific.
var commonOKFields = map[string]bool{
	"action":  true,
	"player":  true,
	"ship":    true,
	"modules": true,
	"message": true,
	// Auto-dock/undock is a side effect the server may attach to ANY command
	// that needs the docked (or undocked) state and finds the ship in the
	// other one — openapi declares these on 87 different response schemas, so
	// they are envelope keys, not per-command fields. Listing them here says
	// "never warn about these"; a command whose caller actually consumes the
	// flag still models it explicitly (see DockResponse.AutoDocked).
	"auto_docked":   true,
	"auto_undocked": true,
}

// actionResponseTypes maps server action strings to their serverapi response
// struct types for unknown field detection.
var actionResponseTypes = map[string]reflect.Type{
	// Info queries
	"get_system":          reflect.TypeOf(serverapi.GetSystemResponse{}),
	"get_poi":             reflect.TypeOf(serverapi.GetPOIResponse{}),
	"get_status":          reflect.TypeOf(serverapi.GetStatusResponse{}),
	"get_ship":            reflect.TypeOf(serverapi.GetShipResponse{}),
	"get_base":            reflect.TypeOf(serverapi.GetBaseResponse{}),
	"get_skills":          reflect.TypeOf(serverapi.GetSkillsResponse{}),
	"get_listings":        reflect.TypeOf(serverapi.GetListingsResponse{}),
	"get_cargo":           reflect.TypeOf(serverapi.GetCargoResponse{}),
	"get_nearby":          reflect.TypeOf(serverapi.GetNearbyResponse{}),
	"get_map":             reflect.TypeOf(serverapi.GetMapResponse{}),
	"get_recipes":         reflect.TypeOf(serverapi.GetRecipesResponse{}),
	"get_wrecks":          reflect.TypeOf(serverapi.GetWrecksResponse{}),
	"get_ships":           reflect.TypeOf(serverapi.GetShipsResponse{}),
	"get_battle_status":   reflect.TypeOf(serverapi.GetBattleStatusResponse{}),
	"get_trades":          reflect.TypeOf(serverapi.GetTradesResponse{}),
	"get_missions":        reflect.TypeOf(serverapi.GetMissionsResponse{}),
	"get_active_missions": reflect.TypeOf(serverapi.GetActiveMissionsResponse{}),
	"get_notes":           reflect.TypeOf(serverapi.GetNotesResponse{}),
	"read_note":           reflect.TypeOf(serverapi.ReadNoteResponse{}),
	"get_commands":        reflect.TypeOf(serverapi.GetCommandsResponse{}),
	"get_guide":           reflect.TypeOf(serverapi.GetGuideResponse{}),
	"get_version":         reflect.TypeOf(serverapi.GetVersionResponse{}),
	"get_action_log":      reflect.TypeOf(serverapi.GetActionLogResponse{}),
	"get_chat_history":    reflect.TypeOf(serverapi.ChatHistoryResponse{}),
	"get_base_cost":       reflect.TypeOf(serverapi.GetBaseCostResponse{}),
	"get_insurance_quote": reflect.TypeOf(serverapi.GetInsuranceQuoteResponse{}),
	"search_systems":      reflect.TypeOf(serverapi.SearchSystemsResponse{}),
	"search_changelog":    reflect.TypeOf(serverapi.SearchChangelogResponse{}),
	"find_route":          reflect.TypeOf(serverapi.FindRouteResponse{}),

	// Market actions
	"view_market":       reflect.TypeOf(serverapi.ViewMarketResponse{}),
	"view_orders":       reflect.TypeOf(serverapi.ViewOrdersResponse{}),
	"buy":               reflect.TypeOf(serverapi.BuyResponse{}),
	"sell":              reflect.TypeOf(serverapi.SellResponse{}),
	"sell_all":          reflect.TypeOf(serverapi.SellResponse{}),
	"create_buy_order":  reflect.TypeOf(serverapi.CreateBuyOrderResponse{}),
	"create_sell_order": reflect.TypeOf(serverapi.CreateSellOrderResponse{}),
	"cancel_order":      reflect.TypeOf(serverapi.CancelOrderResponse{}),
	"modify_order":      reflect.TypeOf(serverapi.ModifyOrderResponse{}),
	"analyze_market":    reflect.TypeOf(serverapi.AnalyzeMarketResponse{}),
	"estimate_purchase": reflect.TypeOf(serverapi.EstimatePurchaseResponse{}),

	// Navigation and resources
	"travel":                 reflect.TypeOf(serverapi.TravelResponse{}),
	"arrived":                reflect.TypeOf(serverapi.TravelResponse{}),
	"jump":                   reflect.TypeOf(serverapi.JumpResponse{}),
	"jumped":                 reflect.TypeOf(serverapi.JumpedResponse{}),
	"mobile_capital_transit": reflect.TypeOf(serverapi.MobileCapitalTransitResponse{}),
	"dock":                   reflect.TypeOf(serverapi.DockResponse{}),
	"undock":                 reflect.TypeOf(serverapi.UndockResponse{}),
	"refuel":                 reflect.TypeOf(serverapi.RefuelResponse{}),
	"repair":                 reflect.TypeOf(serverapi.RepairResponse{}),
	"mine":                   reflect.TypeOf(serverapi.MineResponse{}),
	"craft":                  reflect.TypeOf(serverapi.CraftJobQueued{}),    // was CraftResponse (old instant shape)
	"queue":                  reflect.TypeOf(serverapi.CraftQueueListing{}), // `craft queue` listing — response action is "queue"
	"recycle":                reflect.TypeOf(serverapi.RecycleResponse{}),
	"survey_system":          reflect.TypeOf(serverapi.SurveySystemResponse{}),
	"catalog":                reflect.TypeOf(serverapi.CatalogResponse{}),

	// Combat and scanning
	"cloak":           reflect.TypeOf(serverapi.CloakResponse{}),
	"scan":            reflect.TypeOf(serverapi.ScanResponse{}),
	"battle":          reflect.TypeOf(serverapi.BattleResponse{}),
	"battle_alert":    reflect.TypeOf(serverapi.BattleAlertResponse{}),
	"retreat":         reflect.TypeOf(serverapi.RetreatResponse{}),
	"advance":         reflect.TypeOf(serverapi.AdvanceResponse{}),
	"distress_signal": reflect.TypeOf(serverapi.DistressSignalResponse{}),
	"jettison":        reflect.TypeOf(serverapi.JettisonResponse{}),
	"reload":          reflect.TypeOf(serverapi.ReloadResponse{}),
	"use_item":        reflect.TypeOf(serverapi.UseItemResponse{}),

	// Ship management
	"list_ships":         reflect.TypeOf(serverapi.ListShipsResponse{}),
	"buy_ship":           reflect.TypeOf(serverapi.BuyShipResponse{}),
	"sell_ship":          reflect.TypeOf(serverapi.SellShipResponse{}),
	"switch_ship":        reflect.TypeOf(serverapi.SwitchShipResponse{}),
	"browse_ships":       reflect.TypeOf(serverapi.BrowseShipsResponse{}),
	"buy_listed_ship":    reflect.TypeOf(serverapi.BuyListedShipResponse{}),
	"list_ship_for_sale": reflect.TypeOf(serverapi.ListShipForSaleResponse{}),
	"install_mod":        reflect.TypeOf(serverapi.InstallModResponse{}),
	"uninstall_mod":      reflect.TypeOf(serverapi.UninstallModResponse{}),
	"refit_ship":         reflect.TypeOf(serverapi.RefitShipResponse{}),

	// Notifications
	"get_notifications": reflect.TypeOf(serverapi.GetNotificationsResponse{}),

	// Missions
	"accept_mission":   reflect.TypeOf(serverapi.AcceptMissionResponse{}),
	"complete_mission": reflect.TypeOf(serverapi.CompleteMissionResponse{}),

	// Notes
	"create_note": reflect.TypeOf(serverapi.CreateNoteResponse{}),
	"write_note":  reflect.TypeOf(serverapi.WriteNoteResponse{}),

	// Storage
	"view_storage":   reflect.TypeOf(serverapi.ViewStorageResponse{}),
	"withdraw_items": reflect.TypeOf(serverapi.WithdrawItemsResponse{}),
	"deposit_items":  reflect.TypeOf(serverapi.DepositItemsResponse{}),

	// Achievements
	"get_achievements": reflect.TypeOf(serverapi.GetAchievementsResponse{}),

	// Passengers
	"list_passengers":         reflect.TypeOf(serverapi.ListPassengersResponse{}),
	"load_passenger":          reflect.TypeOf(serverapi.LoadPassengerResponse{}),
	"list_station_passengers": reflect.TypeOf(serverapi.ListStationPassengersResponse{}),
	"unload_passenger":        reflect.TypeOf(serverapi.UnloadPassengerResponse{}),

	// Insurance and commissions
	"buy_insurance":     reflect.TypeOf(serverapi.BuyInsuranceResponse{}),
	"claim_insurance":   reflect.TypeOf(serverapi.ClaimInsuranceResponse{}),
	"commission_quote":  reflect.TypeOf(serverapi.CommissionQuoteResponse{}),
	"commission_ship":   reflect.TypeOf(serverapi.CommissionShipResponse{}),
	"commission_status": reflect.TypeOf(serverapi.CommissionStatusResponse{}),

	// Social
	"trade_offer":  reflect.TypeOf(serverapi.TradeOfferResponse{}),
	"trade_accept": reflect.TypeOf(serverapi.TradeAcceptResponse{}),
	"send_gift":    reflect.TypeOf(serverapi.SendGiftResponse{}),
	"gift_ship":    reflect.TypeOf(serverapi.GiftShipResponse{}),
	"chat":         reflect.TypeOf(serverapi.ChatResponse{}),

	// Forum
	"forum_list":          reflect.TypeOf(serverapi.ForumListResponse{}),
	"forum_get_thread":    reflect.TypeOf(serverapi.ForumThreadResponse{}),
	"forum_create_thread": reflect.TypeOf(serverapi.ForumCreateThreadResponse{}),
	"forum_reply":         reflect.TypeOf(serverapi.ForumReplyResponse{}),

	// Factions
	"faction_info":             reflect.TypeOf(serverapi.FactionInfoResponse{}),
	"faction_list":             reflect.TypeOf(serverapi.FactionListResponse{}),
	"create_faction":           reflect.TypeOf(serverapi.CreateFactionResponse{}),
	"faction_invite":           reflect.TypeOf(serverapi.FactionInviteResponse{}),
	"faction_kick":             reflect.TypeOf(serverapi.FactionKickResponse{}),
	"faction_promote":          reflect.TypeOf(serverapi.FactionPromoteResponse{}),
	"faction_declare_war":      reflect.TypeOf(serverapi.FactionDeclareWarResponse{}),
	"faction_propose_peace":    reflect.TypeOf(serverapi.FactionProposePeaceResponse{}),
	"faction_accept_peace":     reflect.TypeOf(serverapi.FactionAcceptPeaceResponse{}),
	"faction_deposit_credits":  reflect.TypeOf(serverapi.FactionDepositCreditsResponse{}),
	"faction_withdraw_credits": reflect.TypeOf(serverapi.FactionWithdrawCreditsResponse{}),
	"faction_submit_intel":     reflect.TypeOf(serverapi.FactionSubmitIntelResponse{}),
	"espionage":                reflect.TypeOf(serverapi.EspionageResponse{}),
	"view_faction_storage":     reflect.TypeOf(serverapi.ViewFactionStorageResponse{}),
	"faction_accept_invite":    reflect.TypeOf(serverapi.FactionAcceptInviteResponse{}),
	"faction_withdraw_invite":  reflect.TypeOf(serverapi.FactionWithdrawInviteResponse{}),
	"faction_remove_enemy":     reflect.TypeOf(serverapi.FactionRemoveEnemyResponse{}),

	// Empire & citizenship
	"citizenship":     reflect.TypeOf(serverapi.CitizenshipResponse{}),
	"get_empire_info": reflect.TypeOf(serverapi.GetEmpireInfoResponse{}),
	"petition":        reflect.TypeOf(serverapi.PetitionResponse{}),

	// Economy & ship management
	"get_tax_estimate":         reflect.TypeOf(serverapi.GetTaxEstimateResponse{}),
	"get_faction_tax_estimate": reflect.TypeOf(serverapi.FactionTaxEstimateResponse{}),

	// Commands with no dedicated client method, but reachable via play_as's raw
	// passthrough — so responses do arrive and must be typed for drift detection.
	// See serverapi/responses_passthrough.go.
	"faction_garages":           reflect.TypeOf(serverapi.FactionGaragesResponse{}),
	"hunt":                      reflect.TypeOf(serverapi.HuntResponse{}),
	"build_outpost":             reflect.TypeOf(serverapi.BuildBaseResponse{}),
	"buy_ship_license":          reflect.TypeOf(serverapi.ShipLicenseResponse{}),
	"place_ship_buy_order":      reflect.TypeOf(serverapi.PlaceShipBuyOrderResponse{}),
	"cancel_ship_buy_order":     reflect.TypeOf(serverapi.CancelShipBuyOrderResponse{}),
	"view_ship_buy_orders":      reflect.TypeOf(serverapi.ViewShipBuyOrdersResponse{}),
	"sell_ship_to_order":        reflect.TypeOf(serverapi.SellShipToOrderResponse{}),
	"prepay_tax":                reflect.TypeOf(serverapi.PrepayTaxResponse{}),
	"faction_prepay_tax":        reflect.TypeOf(serverapi.FactionPrepayTaxResponse{}),
	"faction_scan_poi":          reflect.TypeOf(serverapi.FactionScanPOIResponse{}),
	"get_faction_achievements":  reflect.TypeOf(serverapi.GetFactionAchievementsResponse{}),
	"get_notification_settings": reflect.TypeOf(serverapi.NotificationSettingsResponse{}),
	"mute_notifications":        reflect.TypeOf(serverapi.NotificationSettingsResponse{}),
	"unmute_notifications":      reflect.TypeOf(serverapi.NotificationSettingsResponse{}),
	"station":                   reflect.TypeOf(serverapi.StationConfigResponse{}),
	"view_insurance":            reflect.TypeOf(serverapi.ViewInsuranceResponse{}),
	"scrap_ship":                reflect.TypeOf(serverapi.ScrapShipResponse{}),
	"dismantle_outpost":         reflect.TypeOf(serverapi.DismantleOutpostResponse{}),
	// Device-login flow (v0.53x). The client authenticates with stored
	// credentials and never opens it, but passthrough can, and login_link_poll's
	// oneOf makes it exactly the kind of shape worth watching for drift.
	"login_link":      reflect.TypeOf(serverapi.LoginLinkResponse{}),
	"login_link_poll": reflect.TypeOf(serverapi.LoginLinkPollResponse{}),

	// Missions, notes & log
	"completed_missions":  reflect.TypeOf(serverapi.CompletedMissionsResponse{}),
	"delete_note":         reflect.TypeOf(serverapi.DeleteNoteResponse{}),
	"captains_log_delete": reflect.TypeOf(serverapi.CaptainsLogDeleteResponse{}),
	"agentlogs":           reflect.TypeOf(serverapi.MessageResponse{}),

	// Bases
	"build_base":      reflect.TypeOf(serverapi.BuildBaseResponse{}),
	"attack_base":     reflect.TypeOf(serverapi.AttackBaseResponse{}),
	"raid_status":     reflect.TypeOf(serverapi.RaidStatusResponse{}),
	"facility":        reflect.TypeOf(serverapi.FacilityResponse{}),
	"types":           reflect.TypeOf(serverapi.FacilityTypesResponse{}),
	"facility_list":   reflect.TypeOf(serverapi.FacilityListResponse{}),
	"list":            reflect.TypeOf(serverapi.FacilityListResponse{}), // shipping's "list" too — see actionExtraResponseTypes
	"browse_for_sale": reflect.TypeOf(serverapi.BrowseForSaleResponse{}),
	"owned":           reflect.TypeOf(serverapi.FacilityOwnedResponse{}),
	"job_list":        reflect.TypeOf(serverapi.CraftQueueListing{}),

	// Wrecks and salvage
	"loot_wreck":    reflect.TypeOf(serverapi.LootWreckResponse{}),
	"salvage_wreck": reflect.TypeOf(serverapi.SalvageWreckResponse{}),
	"tow_wreck":     reflect.TypeOf(serverapi.TowWreckResponse{}),
	"sell_wreck":    reflect.TypeOf(serverapi.SellWreckResponse{}),

	// Drones
	"deploy_drone":   reflect.TypeOf(serverapi.DeployDroneResponse{}),
	"set_drone_name": reflect.TypeOf(serverapi.SetDroneNameResponse{}),

	// Settings
	"set_anonymous": reflect.TypeOf(serverapi.SetAnonymousResponse{}),
	"set_colors":    reflect.TypeOf(serverapi.SetColorsResponse{}),
	"set_status":    reflect.TypeOf(serverapi.SetStatusResponse{}),
	"set_home_base": reflect.TypeOf(serverapi.SetHomeBaseResponse{}),

	// Help
	"help": reflect.TypeOf(serverapi.HelpResponse{}),

	// Self destruct
	"self_destruct": reflect.TypeOf(serverapi.PendingActionResponse{}),

	// Drones
	"get_drone":           reflect.TypeOf(serverapi.GetDroneResponse{}),
	"get_drones":          reflect.TypeOf(serverapi.GetDronesResponse{}),
	"load_drone":          reflect.TypeOf(serverapi.LoadDroneResponse{}),
	"unload_drone":        reflect.TypeOf(serverapi.UnloadDroneResponse{}),
	"recall_drone":        reflect.TypeOf(serverapi.RecallDroneResponse{}),
	"upload_drone_script": reflect.TypeOf(serverapi.UploadDroneScriptResponse{}),

	// Existing structs, previously unmapped
	"faction_list_missions": reflect.TypeOf(serverapi.FactionListMissionsResponse{}),
	"faction_rooms":         reflect.TypeOf(serverapi.FactionRoomsResponse{}),
	"fleet":                 reflect.TypeOf(serverapi.FleetResponse{}),
	"login":                 reflect.TypeOf(serverapi.LoginResponse{}),
	"register":              reflect.TypeOf(serverapi.RegisterResponse{}),
	// Message-only / empty-result actions reuse MessageResponse
	"claim":                  reflect.TypeOf(serverapi.MessageResponse{}),
	"leave_faction":          reflect.TypeOf(serverapi.MessageResponse{}),
	"logout":                 reflect.TypeOf(serverapi.MessageResponse{}),
	"trade_cancel":           reflect.TypeOf(serverapi.MessageResponse{}),
	"trade_decline":          reflect.TypeOf(serverapi.MessageResponse{}),
	"faction_deposit_items":  reflect.TypeOf(serverapi.MessageResponse{}),
	"faction_withdraw_items": reflect.TypeOf(serverapi.MessageResponse{}),
	"session":                reflect.TypeOf(serverapi.MessageResponse{}),
	// Missions & captain's log
	"abandon_mission":        reflect.TypeOf(serverapi.AbandonMissionResponse{}),
	"decline_mission":        reflect.TypeOf(serverapi.DeclineMissionResponse{}),
	"view_completed_mission": reflect.TypeOf(serverapi.ViewCompletedMissionResponse{}),
	"captains_log_add":       reflect.TypeOf(serverapi.CaptainsLogAddResponse{}),
	"captains_log_get":       reflect.TypeOf(serverapi.CaptainsLogGetResponse{}),
	"captains_log_list":      reflect.TypeOf(serverapi.CaptainsLogListResponse{}),
	// Ship, combat & wreck
	"attack":              reflect.TypeOf(serverapi.AttackResponse{}),
	"name_ship":           reflect.TypeOf(serverapi.NameShipResponse{}),
	"release_tow":         reflect.TypeOf(serverapi.ReleaseTowResponse{}),
	"repair_module":       reflect.TypeOf(serverapi.RepairModuleResponse{}),
	"scrap_wreck":         reflect.TypeOf(serverapi.ScrapWreckResponse{}),
	"cancel_ship_listing": reflect.TypeOf(serverapi.CancelShipListingResponse{}),
	// Commission, forum & agents
	"cancel_commission":   reflect.TypeOf(serverapi.CancelCommissionResponse{}),
	"supply_commission":   reflect.TypeOf(serverapi.SupplyCommissionResponse{}),
	"forum_delete_reply":  reflect.TypeOf(serverapi.ForumDeleteReplyResponse{}),
	"forum_delete_thread": reflect.TypeOf(serverapi.ForumDeleteThreadResponse{}),
	"forum_upvote":        reflect.TypeOf(serverapi.ForumUpvoteResponse{}),
	"get_system_agents":   reflect.TypeOf(serverapi.GetSystemAgentsResponse{}),
	// Faction roles, rooms & membership
	"join_faction":           reflect.TypeOf(serverapi.JoinFactionResponse{}),
	"faction_decline_invite": reflect.TypeOf(serverapi.FactionDeclineInviteResponse{}),
	"faction_get_invites":    reflect.TypeOf(serverapi.FactionGetInvitesResponse{}),
	"faction_create_role":    reflect.TypeOf(serverapi.FactionCreateRoleResponse{}),
	"faction_delete_role":    reflect.TypeOf(serverapi.FactionDeleteRoleResponse{}),
	"faction_edit_role":      reflect.TypeOf(serverapi.FactionEditRoleResponse{}),
	"faction_edit":           reflect.TypeOf(serverapi.FactionEditResponse{}),
	"faction_delete_room":    reflect.TypeOf(serverapi.FactionDeleteRoomResponse{}),
	"faction_visit_room":     reflect.TypeOf(serverapi.FactionVisitRoomResponse{}),
	"faction_write_room":     reflect.TypeOf(serverapi.FactionWriteRoomResponse{}),
	// Faction allies, enemies & intel
	"faction_accept_ally":        reflect.TypeOf(serverapi.FactionAcceptAllyResponse{}),
	"faction_propose_ally":       reflect.TypeOf(serverapi.FactionProposeAllyResponse{}),
	"faction_remove_ally":        reflect.TypeOf(serverapi.FactionRemoveAllyResponse{}),
	"faction_set_enemy":          reflect.TypeOf(serverapi.FactionSetEnemyResponse{}),
	"faction_intel_status":       reflect.TypeOf(serverapi.FactionIntelStatusResponse{}),
	"faction_query_intel":        reflect.TypeOf(serverapi.FactionQueryIntelResponse{}),
	"faction_query_trade_intel":  reflect.TypeOf(serverapi.FactionQueryTradeIntelResponse{}),
	"faction_submit_trade_intel": reflect.TypeOf(serverapi.FactionSubmitTradeIntelResponse{}),
	"faction_trade_intel_status": reflect.TypeOf(serverapi.FactionTradeIntelStatusResponse{}),
	// Faction orders & missions
	"faction_create_buy_order":  reflect.TypeOf(serverapi.FactionCreateBuyOrderResponse{}),
	"faction_create_sell_order": reflect.TypeOf(serverapi.FactionCreateSellOrderResponse{}),
	"faction_post_mission":      reflect.TypeOf(serverapi.FactionPostMissionResponse{}),
	"faction_cancel_mission":    reflect.TypeOf(serverapi.FactionCancelMissionResponse{}),

	// /shipping reads (v0.531.4). The endpoint is action-dispatched, so the
	// reply's top-level action is a BARE verb with no marker tying it back to
	// /shipping — same invariant as the shipping_<action> storeRawJSON keys in
	// client.go: this only works while no other command replies with these
	// verbs. Shipping MUTATIONS (accept/deliver/return/cancel/post/pay_debt)
	// arrive as tick-deferred action_result frames, which this monitor does
	// not field-check, so only the synchronous reads are registered.
	"get":     reflect.TypeOf(serverapi.ShippingContractResponse{}),
	"profile": reflect.TypeOf(serverapi.ShippingProfileResponse{}),
	"track":   reflect.TypeOf(serverapi.ShippingTrackResponse{}),
}

// actionExtraResponseTypes lists ADDITIONAL response structs for actions whose
// verb is served by more than one command. The expected-field set for such an
// action is the UNION of all its structs' fields: a genuinely new server field
// (in none of them) still fires, and the only masking risk is one command
// growing a field that happens to share a name with another command's — which
// does not exist today. Needed because the payload carries no originating
// command, so the shapes cannot be told apart at the monitor.
var actionExtraResponseTypes = map[string][]reflect.Type{
	// "list" is both the facility list and the /shipping board read.
	"list": {reflect.TypeOf(serverapi.ShippingListResponse{})},
}

// eventExpectedFields maps non-OK event types to their expected payload fields.
// Event types not listed here skip field-level checking.
var eventExpectedFields = map[string]map[string]bool{
	protocol.TypeWelcome: {
		"current_tick":  true,
		"version":       true,
		"message":       true,
		"game_info":     true,
		"help_text":     true,
		"motd":          true,
		"release_date":  true,
		"release_notes": true,
		"server_time":   true,
		"terms":         true,
		"tick_rate":     true,
		"website":       true,
	},
	protocol.TypeRegistered: {
		"password":       true,
		"token":          true,
		"player_id":      true,
		"message":        true,
		"player":         true,
		"ship":           true,
		"system":         true,
		"poi":            true,
		"captains_log":   true,
		"pending_trades": true,
		"release_info":   true,
		"unread_chat":    true,
	},
	protocol.TypeLoggedIn: {
		"message":        true,
		"player":         true,
		"ship":           true,
		"modules":        true,
		"system":         true,
		"poi":            true,
		"captains_log":   true,
		"pending_trades": true,
		"release_info":   true,
		"unread_chat":    true,
		"recent_chat":    true,
	},
	protocol.TypeStateUpdate: {
		"tick":      true,
		"in_combat": true,
		"player":    true,
		"ship":      true,
		"modules":   true,
		"travel":    true,
		"nearby":    true,
	},
	protocol.TypeTick: {
		"tick": true,
	},
	protocol.TypeDocked: {
		"base_id":   true,
		"base_name": true,
		"message":   true,
		"story":     true,
		"poi":       true,
		"poi_id":    true,
	},
	protocol.TypeUndocked: {
		"message": true,
	},
	protocol.TypeActionResult: {
		"action":        true,
		"message":       true,
		"success":       true,
		"result":        true,
		"command":       true,
		"tick":          true,
		// Both flags since the 2026-07-20 patch: any command needing a
		// different dock state auto-transitions same-tick and reports which
		// way it went.
		"auto_docked":   true,
		"auto_undocked": true,
	},
	protocol.TypeError: {
		"message":         true,
		"code":            true,
		"pending_command": true,
	},
	protocol.TypeActionError: {
		"action":  true,
		"message": true,
		"code":    true,
		"command": true,
		"tick":    true,
		// Structured error breakdown, e.g. commission_ship/craft
		// missing_materials lists per-item have/need.
		"details": true,
	},
	protocol.TypeReconnected: {
		"message":         true,
		"ticks_remaining": true,
		"was_pilotless":   true,
	},
	protocol.TypePoliceSpawn: {
		"message":    true,
		"num_drones": true,
		"target":     true,
	},
	protocol.TypeListings: {
		"listings":     true,
		"station_id":   true,
		"station_name": true,
	},
	protocol.TypeMiningYield: {
		"resource_id":       true,
		"quantity":          true,
		"message":           true,
		"depletion_percent": true,
		"max_remaining":     true,
		"remaining":         true,
		"remaining_display": true,
		"resource_name":     true,
		"xp_gained":         true,
		"drone_id":          true, // present when the yield came from a deployed drone
	},
	protocol.TypeCraftingUpdate: {
		"tick": true,
		"jobs": true,
	},
	protocol.TypePirateWarning: {
		"pirate_name": true,
		"pirate_tier": true,
		"pirate_id":   true,
		"message":     true,
	},
	protocol.TypePirateCombat: {
		"pirate_name": true,
		"pirate_tier": true,
		"pirate_id":   true,
		"damage":      true,
		"damage_type": true,
		"your_hull":   true,
		"your_shield": true,
		"message":     true,
	},
	protocol.TypePirateDestroyed: {
		"pirate_name": true,
		"pirate_tier": true,
		"pirate_id":   true,
		"loot":        true,
		"message":     true,
		"xp_gained":   true,
		"pirate_role": true,
		"is_boss":     true,
		"killer":      true,
		"system_id":   true,
		"system_name": true,
	},
	protocol.TypeCombatUpdate: {
		"damage":      true,
		"damage_type": true,
		"message":     true,
	},
	protocol.TypePlayerDied: {
		"message":          true,
		"respawn_at":       true,
		"credits_lost":     true,
		"cause":            true,
		"killer_id":        true,
		"killer_name":      true,
		"respawn_base":     true,
		"clone_cost":       true,
		"insurance_payout": true,
		"ship_lost":        true,
		"new_ship_class":   true,
		"wreck_id":         true,
		"combat_log":       true,
	},
	protocol.TypeBattleStarted: {
		"battle_id":    true,
		"system_id":    true,
		"message":      true,
		"participants": true,
		"sides":        true,
	},
	protocol.TypeBattleUpdate: {
		"battle_id":      true,
		"tick":           true,
		"auto_pilot":     true,
		"participants":   true,
		"sides":          true,
		"your_side_id":   true,
		"your_stance":    true,
		"your_target_id": true,
		"your_zone":      true,
	},
	protocol.TypeBattleDamage: {
		"tick":          true,
		"attacker_id":   true,
		"attacker_name": true,
		"target_id":     true,
		"target_name":   true,
		"damage_type":   true,
		"hit_success":   true,
		"hull_hit":      true,
		"shield_hit":    true,
		"total_damage":  true,
		"weapons_fired": true,
		"xp_gained":     true,
	},
	protocol.TypeChatMessage: {
		"channel":         true,
		"content":         true,
		"id":              true,
		"sender":          true,
		"sender_id":       true,
		"target_id":       true,
		"target_name":     true,
		"faction_id":      true,
		"timestamp":       true,
		"distress_type":   true,
		"system":          true,
		"system_id":       true,
		"poi_id":          true,
		"empire_official": true,
	},
	protocol.TypeSkillLevelUp: {
		"skill_id":   true,
		"skill_name": true,
		"new_level":  true,
		"xp_gained":  true,
		"message":    true,
	},
	protocol.TypeGameplayTip: {
		"message": true,
		"tip":     true,
	},
	protocol.TypeFactionPromote: {
		"faction_name": true,
		"new_role":     true,
		"old_role":     true,
		"promoted_by":  true,
		"message":      true,
	},
	protocol.TypeFactionInvite: {
		"faction_id":   true,
		"faction_name": true,
		"invited_by":   true,
		"message":      true,
	},
	protocol.TypeTradeOfferReceived: {
		"trade_id": true,
		"from":     true,
		"message":  true,
	},
	protocol.TypeCompleteMission: {
		"mission_id":    true,
		"mission_title": true,
		"rewards":       true,
		"message":       true,
	},
}

// actionFieldsCache caches the computed expected field sets per action to
// avoid repeated reflection on every response.
var actionFieldsCache sync.Map // map[string]map[string]bool

// jsonFieldNames extracts top-level JSON field names from a struct type's tags,
// including promoted fields from embedded structs.
func jsonFieldNames(t reflect.Type) map[string]bool {
	names := make(map[string]bool)
	for i := range t.NumField() {
		field := t.Field(i)
		if field.Anonymous && field.Type.Kind() == reflect.Struct {
			for k, v := range jsonFieldNames(field.Type) {
				names[k] = v
			}
			continue
		}
		tag := field.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if name != "" {
			names[name] = true
		}
	}
	return names
}

// expectedFieldsForAction returns the combined set of expected fields for an
// action: the struct's JSON tags plus common envelope fields. Results are cached.
func expectedFieldsForAction(action string) (map[string]bool, bool) {
	if cached, ok := actionFieldsCache.Load(action); ok {
		return cached.(map[string]bool), true
	}
	responseType, known := actionResponseTypes[action]
	if !known {
		return nil, false
	}
	fields := jsonFieldNames(responseType)
	for _, extra := range actionExtraResponseTypes[action] {
		for k := range jsonFieldNames(extra) {
			fields[k] = true
		}
	}
	for k := range commonOKFields {
		fields[k] = true
	}
	actionFieldsCache.Store(action, fields)
	return fields, true
}

// actionStructNames names every struct registered for an action, for warning
// messages on union-checked actions.
func actionStructNames(action string) string {
	names := []string{actionResponseTypes[action].Name()}
	for _, extra := range actionExtraResponseTypes[action] {
		names = append(names, extra.Name())
	}
	return strings.Join(names, " or ")
}

// checkForAPIChanges inspects a server response for unhandled types or unknown fields.
func (c *Client) checkForAPIChanges(resp protocol.Response) {
	if len(resp.Payload) == 0 {
		return
	}

	switch resp.Type {
	case protocol.TypeOK:
		CheckOKResponseFields(resp.Payload)
	default:
		c.checkEventFields(resp.Type, resp.Payload)
	}
}

// CheckOKResponseFields checks a TypeOK response for unknown actions or unknown fields.
// Exported so both WebSocket and MCP transports can use it.
func CheckOKResponseFields(payload map[string]any) {
	action, _ := payload["action"].(string)
	if action == "" {
		return
	}

	expectedFields, known := expectedFieldsForAction(action)
	if !known {
		key := "unknown_action:" + action
		if _, loaded := loggedAPIChanges.LoadOrStore(key, true); !loaded {
			payloadJSON, _ := json.Marshal(payload)
			apiChangeLogger.Printf(
				"Unhandled action %q in OK response - add to actionResponseTypes in client_api_monitor.go and create serverapi struct.\nFull payload: %s",
				action, string(payloadJSON))
		}
		return
	}

	var unknown []string
	for key := range payload {
		if !expectedFields[key] {
			unknown = append(unknown, key)
		}
	}

	if len(unknown) > 0 {
		sort.Strings(unknown)
		key := "unknown_fields:" + action + ":" + strings.Join(unknown, ",")
		if _, loaded := loggedAPIChanges.LoadOrStore(key, true); !loaded {
			apiChangeLogger.Printf(
				"New fields in %q response not in %s: %v - update the serverapi struct",
				action, actionStructNames(action), unknown)
		}
	}
}

// checkEventFields checks a non-OK event response for unknown fields.
func (c *Client) checkEventFields(eventType string, payload map[string]any) {
	expectedFields, known := eventExpectedFields[eventType]
	if !known {
		// Not in our expected fields map — skip field-level checking.
		// Truly unknown types are logged by logUnhandledResponseType.
		return
	}

	var unknown []string
	for key := range payload {
		if !expectedFields[key] {
			unknown = append(unknown, key)
		}
	}

	if len(unknown) > 0 {
		sort.Strings(unknown)
		key := "unknown_event_fields:" + eventType + ":" + strings.Join(unknown, ",")
		if _, loaded := loggedAPIChanges.LoadOrStore(key, true); !loaded {
			apiChangeLogger.Printf(
				"New fields in %q event not currently handled: %v - update eventExpectedFields in client_api_monitor.go",
				eventType, unknown)
		}
	}
}

// logUnhandledResponseType logs full details about a server response type
// that has no handler in handleResponse.
func logUnhandledResponseType(resp protocol.Response) {
	key := "unknown_type:" + resp.Type
	if _, loaded := loggedAPIChanges.LoadOrStore(key, true); loaded {
		return
	}
	payloadJSON, _ := json.Marshal(resp.Payload)
	apiChangeLogger.Printf(
		"Unhandled response type %q - add case to handleResponse() and constant to protocol/messages.go.\nFull payload: %s",
		resp.Type, string(payloadJSON))
}
