package main

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/rsned/spacemolt/internal/protocol"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// pushEventLines renders an unsolicited server push as terminal lines.
//
// The client decodes the whole combat family into serverapi structs and then
// logs it at DEBUG level only, so without `set_debug true` a session watching
// a fight sees nothing but its own command echoes — explorer-8 fought and lost
// a creature battle at wasat_cryobelt with no on-screen sign of the exchange.
// These are the frames that carry information the pilot must act on.
//
// selfID is the player id, used to read a damage frame from our own side.
// A nil return means "not worth printing": frames already surfaced elsewhere
// (chat, crafting), per-tick state noise, and replies to our own commands
// (RequestID set), which the command path prints.
func pushEventLines(resp protocol.Response, selfID string) []string {
	if resp.RequestID != "" {
		return nil
	}

	switch resp.Type {
	case protocol.TypeBattleDamage:
		return battleDamageLines(resp.Payload, selfID)
	case protocol.TypeBattleUpdate:
		return battleUpdateLines(resp.Payload, selfID)
	case protocol.TypeBattleStarted:
		return battleStartedLines(resp.Payload)
	case protocol.TypeBattleEnded:
		return battleEndedLines(resp.Payload)
	case protocol.TypeBattleJoined:
		return battleJoinedLines(resp.Payload)
	case protocol.TypeBattleLeft:
		return battleLeftLines(resp.Payload)
	case protocol.TypeCombatUpdate:
		return combatUpdateLines(resp.Payload)
	case protocol.TypePlayerDied:
		return playerDiedLines(resp.Payload)
	case protocol.TypePlayerKill:
		return playerKillLines(resp.Payload)
	case protocol.TypeShipCaptured:
		return shipCapturedLines(resp.Payload)
	case protocol.TypePirateWarning:
		return pirateWarningLines(resp.Payload)
	case protocol.TypePirateDestroyed:
		return pirateDestroyedLines(resp.Payload)
	case protocol.TypePoliceWarning:
		return policeWarningLines(resp.Payload)
	case protocol.TypeSkillLevelUp:
		return skillLevelUpLines(resp.Payload)
	case protocol.TypeServerRestartWarning:
		return serverRestartLines(resp.Payload)
	}
	return nil
}

// decodePush re-marshals a payload map into ev. Push payloads are small, and
// this keeps every renderer reading the same serverapi structs the client
// decodes, rather than hand-picking map keys.
func decodePush(payload map[string]any, ev any) bool {
	data, err := json.Marshal(payload)
	if err != nil {
		return false
	}
	return json.Unmarshal(data, ev) == nil
}

// num renders a float that is usually a whole number without a decimal point
// or an exponent (%g turns 1000000 into "1e+06").
func num(v float64) string {
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return fmt.Sprintf("%.0f", v)
	}
	return fmt.Sprintf("%g", v)
}

// wreckLine renders the shared "where the wreck is and what is in it" tail
// carried by player_died, player_kill and pirate_destroyed (v0.574.x).
func wreckLine(id, poiName, systemName string, hasCargo, hasModules bool) string {
	if id == "" {
		return ""
	}
	where := "wreck " + id
	switch {
	case poiName != "" && systemName != "":
		where += " at " + poiName + ", " + systemName
	case poiName != "":
		where += " at " + poiName
	case systemName != "":
		where += " in " + systemName
	}
	segs := []string{where}
	if hasCargo {
		segs = append(segs, "has cargo")
	}
	if hasModules {
		segs = append(segs, "has modules")
	}
	return "   " + strings.Join(segs, " · ")
}

func battleDamageLines(payload map[string]any, selfID string) []string {
	var ev serverapi.BattleDamage
	if !decodePush(payload, &ev) {
		return nil
	}
	attacker := firstNonEmpty(ev.AttackerName, ev.AttackerID)
	target := firstNonEmpty(ev.TargetName, ev.TargetID)
	if selfID != "" {
		if ev.AttackerID == selfID {
			attacker = "you"
		}
		if ev.TargetID == selfID {
			target = "you"
		}
	}
	if !ev.HitSuccess {
		return []string{fmt.Sprintf("⚔ %s → %s: MISS", attacker, target)}
	}
	dmg := num(ev.TotalDamage)
	if ev.DamageType != "" {
		dmg += " " + ev.DamageType
	}
	return []string{fmt.Sprintf("⚔ %s → %s: %s (shield %s, hull %s)",
		attacker, target, dmg, num(ev.ShieldHit), num(ev.HullHit))}
}

func battleUpdateLines(payload map[string]any, selfID string) []string {
	var ev serverapi.BattleUpdate
	if !decodePush(payload, &ev) {
		return nil
	}

	// Resolve our target id to a name from the roster the same frame carries.
	target := ev.YourTargetID
	for _, p := range ev.Participants {
		if p.PlayerID == ev.YourTargetID && p.Username != "" {
			target = p.Username
			break
		}
	}

	var head strings.Builder
	head.WriteString("⚔")
	if ev.Tick > 0 {
		fmt.Fprintf(&head, " tick %d", ev.Tick)
	}
	var you []string
	if ev.YourSideID != 0 {
		you = append(you, fmt.Sprintf("side %d", ev.YourSideID))
	}
	if ev.YourStance != "" {
		you = append(you, "stance "+ev.YourStance)
	}
	if ev.YourZone != "" {
		you = append(you, "zone "+ev.YourZone)
	}
	if target != "" {
		you = append(you, "target "+target)
	}
	if len(you) > 0 {
		fmt.Fprintf(&head, " — you: %s", strings.Join(you, ", "))
	}
	if ev.AutoPilot {
		head.WriteString(" [autopilot]")
	}

	lines := []string{head.String()}
	for _, p := range ev.Participants {
		name := firstNonEmpty(p.Username, p.PlayerID)
		line := fmt.Sprintf("   side %d: %s hull %d%% shield %d%%", p.SideID, name, p.HullPct, p.ShieldPct)
		if selfID != "" && p.PlayerID == selfID {
			line += "  ← you"
		}
		lines = append(lines, line)
	}
	return lines
}

func battleStartedLines(payload map[string]any) []string {
	var ev serverapi.BattleStarted
	if !decodePush(payload, &ev) {
		return nil
	}
	segs := []string{"⚔ Battle started"}
	if ev.BattleID != "" {
		segs = append(segs, "battle="+ev.BattleID)
	}
	if ev.SystemID != "" {
		segs = append(segs, "system="+ev.SystemID)
	}
	if n := len(ev.Participants); n > 0 {
		segs = append(segs, fmt.Sprintf("combatants=%d", n))
	}
	return []string{strings.Join(segs, " ")}
}

func battleEndedLines(payload map[string]any) []string {
	var ev serverapi.BattleEnded
	if !decodePush(payload, &ev) {
		return nil
	}
	segs := []string{"⚔ Battle ended"}
	if ev.BattleID != "" {
		segs = append(segs, "battle="+ev.BattleID)
	}
	if ev.Reason != "" {
		segs = append(segs, "reason="+ev.Reason)
	}
	// winning_side is -1 for a stalemate; printing the raw -1 reads as a bug.
	if ev.WinningSide < 0 {
		segs = append(segs, "winner=stalemate")
	} else {
		segs = append(segs, fmt.Sprintf("winner=side %d", ev.WinningSide))
	}
	if ev.Duration > 0 {
		segs = append(segs, fmt.Sprintf("ticks=%d", ev.Duration))
	}
	if ev.ShipsDestroyed > 0 {
		segs = append(segs, fmt.Sprintf("destroyed=%d", ev.ShipsDestroyed))
	}
	if ev.ShipsCaptured > 0 {
		segs = append(segs, fmt.Sprintf("captured=%d", ev.ShipsCaptured))
	}
	if ev.TotalDamage > 0 {
		segs = append(segs, "damage="+num(ev.TotalDamage))
	}
	return []string{strings.Join(segs, " ")}
}

func battleJoinedLines(payload map[string]any) []string {
	var ev serverapi.BattleJoined
	if !decodePush(payload, &ev) {
		return nil
	}
	return []string{fmt.Sprintf("⚔ %s joined side %d", firstNonEmpty(ev.Username, ev.PlayerID), ev.SideID)}
}

func battleLeftLines(payload map[string]any) []string {
	var ev serverapi.BattleLeft
	if !decodePush(payload, &ev) {
		return nil
	}
	// A creature leaves with an empty username, so fall back to the crt_ id.
	who := firstNonEmpty(ev.Username, ev.PlayerID)
	if ev.Reason == "" {
		return []string{fmt.Sprintf("⚔ %s left", who)}
	}
	return []string{fmt.Sprintf("⚔ %s left (%s)", who, ev.Reason)}
}

func combatUpdateLines(payload map[string]any) []string {
	var ev serverapi.CombatUpdate
	if !decodePush(payload, &ev) {
		return nil
	}
	dmg := num(ev.Damage)
	if ev.DamageType != "" {
		dmg += " " + ev.DamageType
	}
	line := fmt.Sprintf("⚔ %s → %s: %s (shield %s, hull %s)",
		ev.Attacker, ev.Target, dmg, num(ev.ShieldHit), num(ev.HullHit))
	if ev.Destroyed {
		line += " DESTROYED"
	}
	return []string{line}
}

func playerDiedLines(payload map[string]any) []string {
	var ev serverapi.PlayerDied
	if !decodePush(payload, &ev) {
		return nil
	}
	head := "💀 Destroyed"
	if ev.KillerName != "" {
		head += " by " + ev.KillerName
	}
	if ev.Cause != "" {
		head += " (" + ev.Cause + ")"
	}
	var lost []string
	if ev.ShipLost != "" {
		lost = append(lost, "lost "+ev.ShipLost)
	}
	if ev.NewShipClass != "" {
		lost = append(lost, "now in "+ev.NewShipClass)
	}
	if len(lost) > 0 {
		head += " — " + strings.Join(lost, ", ")
	}
	lines := []string{head}

	var segs []string
	if ev.WreckSuppressed {
		segs = append(segs, "no wreck left")
	} else if w := wreckLine(ev.WreckID, ev.WreckPOIName, ev.WreckSystemName, false, false); w != "" {
		segs = append(segs, strings.TrimPrefix(w, "   "))
	}
	if ev.RespawnBase != "" {
		segs = append(segs, "respawn "+ev.RespawnBase)
	}
	if ev.CloneCost > 0 {
		segs = append(segs, fmt.Sprintf("clone %d", ev.CloneCost))
	}
	if ev.InsurancePayout > 0 {
		segs = append(segs, fmt.Sprintf("insurance %d", ev.InsurancePayout))
	}
	if len(segs) > 0 {
		lines = append(lines, "   "+strings.Join(segs, " · "))
	}
	return lines
}

func playerKillLines(payload map[string]any) []string {
	var ev serverapi.PlayerKill
	if !decodePush(payload, &ev) {
		return nil
	}
	lines := []string{"💥 Destroyed " + ev.Victim}
	if w := wreckLine(ev.WreckID, ev.WreckPOIName, ev.WreckSystemName, ev.WreckHasCargo, ev.WreckHasModules); w != "" {
		lines = append(lines, w)
	}
	return lines
}

func shipCapturedLines(payload map[string]any) []string {
	var ev serverapi.ShipCaptured
	if !decodePush(payload, &ev) {
		return nil
	}
	captor := firstNonEmpty(ev.CaptorUsername, ev.CaptorID)
	owner := firstNonEmpty(ev.FormerOwnerUsername, ev.FormerOwnerID)
	hull := firstNonEmpty(ev.ShipClass, ev.ShipID)
	return []string{fmt.Sprintf("🏴 %s captured %s from %s", captor, hull, owner)}
}

func pirateWarningLines(payload map[string]any) []string {
	var ev serverapi.PirateWarning
	if !decodePush(payload, &ev) {
		return nil
	}
	var detail []string
	if ev.PirateName != "" {
		detail = append(detail, ev.PirateName)
	}
	if ev.Tier != "" {
		detail = append(detail, ev.Tier)
	}
	if ev.IsBoss {
		detail = append(detail, "BOSS")
	}
	if ev.AttackInTicks > 0 {
		detail = append(detail, fmt.Sprintf("attacks in %d ticks", ev.AttackInTicks))
	}
	return []string{"🏴 " + joinMessageDetail(ev.Message, detail)}
}

func pirateDestroyedLines(payload map[string]any) []string {
	var ev serverapi.PirateDestroyed
	if !decodePush(payload, &ev) {
		return nil
	}
	head := "🏴 Destroyed " + firstNonEmpty(ev.PirateName, ev.PirateID)
	if ev.Tier != "" {
		head += " (" + ev.Tier + ")"
	}
	var reward []string
	if ev.CreditsReward > 0 {
		reward = append(reward, fmt.Sprintf("%d credits", ev.CreditsReward))
	}
	if ev.XPGained > 0 {
		reward = append(reward, num(ev.XPGained)+" xp")
	}
	if len(reward) > 0 {
		head += " — " + strings.Join(reward, ", ")
	}
	lines := []string{head}
	if w := wreckLine(ev.WreckID, ev.WreckPOIName, ev.WreckSystemName, ev.WreckHasCargo, ev.WreckHasModules); w != "" {
		lines = append(lines, w)
	}
	return lines
}

func policeWarningLines(payload map[string]any) []string {
	var ev serverapi.PoliceWarning
	if !decodePush(payload, &ev) {
		return nil
	}
	var detail []string
	if ev.PoliceLevel > 0 {
		detail = append(detail, fmt.Sprintf("level %d", ev.PoliceLevel))
	}
	if ev.System != "" {
		detail = append(detail, ev.System)
	}
	if ev.ResponseTicks > 0 {
		detail = append(detail, fmt.Sprintf("responding in %d ticks", ev.ResponseTicks))
	}
	return []string{"🚓 " + joinMessageDetail(ev.Message, detail)}
}

func skillLevelUpLines(payload map[string]any) []string {
	var ev serverapi.SkillLevelUp
	if !decodePush(payload, &ev) {
		return nil
	}
	if ev.SkillID == "" {
		return nil
	}
	return []string{fmt.Sprintf("⭐ %s reached level %d", ev.SkillID, ev.NewLevel)}
}

func serverRestartLines(payload map[string]any) []string {
	var ev serverapi.ServerRestartWarning
	if !decodePush(payload, &ev) {
		return nil
	}
	line := fmt.Sprintf("restart in %ds", ev.SecondsUntilRestart)
	if ev.TargetVersion != "" {
		line += " (target " + ev.TargetVersion + ")"
	}
	if ev.Message != "" {
		line = ev.Message + " — " + line
	}
	return []string{"⚠ " + line}
}

// joinMessageDetail renders "<message> (<detail, detail>)", degrading to
// whichever half is present.
func joinMessageDetail(message string, detail []string) string {
	joined := strings.Join(detail, ", ")
	switch {
	case message != "" && joined != "":
		return message + " (" + joined + ")"
	case message != "":
		return message
	default:
		return joined
	}
}

// pushEventColor picks a colour by severity so a death or a capture does not
// scroll past looking like another damage tick.
func pushEventColor(typ string) string {
	switch typ {
	case protocol.TypePlayerDied, protocol.TypePlayerKill, protocol.TypeShipCaptured:
		return ansiBrightRed
	case protocol.TypePirateWarning, protocol.TypePirateDestroyed, protocol.TypePoliceWarning:
		return ansiMagenta
	case protocol.TypeSkillLevelUp:
		return ansiGreen
	case protocol.TypeServerRestartWarning:
		return ansiBrightYellow
	default:
		return ansiYellow
	}
}

// printPushEvent writes a push event above the prompt. The leading \r returns
// to column 0 so the line does not land in the middle of whatever the user has
// typed, matching the crafting-update handler.
func printPushEvent(resp protocol.Response, selfID string) {
	lines := pushEventLines(resp, selfID)
	if len(lines) == 0 {
		return
	}
	color := pushEventColor(resp.Type)
	for _, line := range lines {
		fmt.Printf("\r%s%s%s\n", color, line, ansiReset)
	}
}

// showPushEvents gates push rendering at runtime via `set_events`. Long
// battles push a battle_update and a battle_damage every tick, which is what
// you want while fighting and noise while scripting.
var showPushEvents atomic.Bool

// parseOnOff accepts the on/off spellings alongside everything
// strconv.ParseBool takes, for the set_* toggles.
func parseOnOff(s string) (bool, error) {
	switch strings.ToLower(s) {
	case "on":
		return true, nil
	case "off":
		return false, nil
	}
	return strconv.ParseBool(s)
}

// enabledWord renders a toggle state for the set_* confirmations.
func enabledWord(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}
