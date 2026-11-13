package main

import (
	"context"
	"log"
	"path/filepath"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/mbox"
	"github.com/rsned/spacemolt/pkg/worker"
)

// sweepChatChannels are the chat channels a landing pulls: uncharted systems
// broadcast encoded messages (2026-11-13), and both are scoped to where the
// ship is.
var sweepChatChannels = []string{"system", "local"}

// sweepChatPerChannel bounds one landing's history walk per channel.
const sweepChatPerChannel = 50

// wireSweepMbox makes a Pathfinder sweep worker keep what the server tells
// it: every chat push and every non-routine push (game.Client.SetOnPush) is
// stored in data/agents/<id>/mbox.db and logged, and each landing pulls the
// system's chat history (dispatch.ChatBackfill). It returns the store's
// close func, or nil when the mbox could not be opened.
func wireSweepMbox(client *game.Client, d *worker.WorkerDispatch, agentID string, logger *log.Logger) func() {
	store, err := mbox.Open(filepath.Join("data", "agents", agentID, "mbox.db"))
	if err != nil {
		logger.Printf("warning: sweep mbox: %v (pushes not recorded)", err)
		return nil
	}
	ing := mbox.NewIngester(store)
	if st := client.GetState(); st != nil && st.Player.ID != "" {
		ing.SetSelfID(st.Player.ID)
	}
	client.SetOnChatMessage(func(msg serverapi.ChatMessage) {
		ing.HandlePush(msg)
		logger.Printf("chat [%s] %s: %s", msg.Channel, msg.Sender, msg.Content)
	})
	client.SetOnPush(func(typ string, payload map[string]any) {
		ing.HandleEvent(typ, payload)
		msg, _ := payload["message"].(string)
		logger.Printf("push %s: %s", typ, msg)
	})
	d.ChatBackfill = func(ctx context.Context) (int, error) {
		rep, err := ing.Backfill(ctx, client, mbox.BackfillOptions{Channels: sweepChatChannels, MaxPerChannel: sweepChatPerChannel})
		n := 0
		for _, c := range rep.Channels {
			n += c.Fetched
		}
		return n, err
	}
	return func() { _ = store.Close() }
}
