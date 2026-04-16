package mbox

import (
	"log"
	"time"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// Ingester converts incoming server push events into stored Messages.
type Ingester struct {
	store  *Store
	logger *log.Logger
}

// NewIngester creates an Ingester backed by the given Store.
func NewIngester(store *Store) *Ingester {
	return &Ingester{
		store:  store,
		logger: log.New(log.Default().Writer(), "[mbox] ", log.LstdFlags),
	}
}

// HandlePush converts a server-pushed ChatMessage and stores it in the mbox.
// Duplicate messages (same ID) are silently ignored.
func (ing *Ingester) HandlePush(msg serverapi.ChatMessage) {
	ts, err := time.Parse(time.RFC3339, msg.TimestampUTC)
	if err != nil {
		ts, err = time.Parse(time.RFC3339Nano, msg.TimestampUTC)
		if err != nil {
			ts = time.Now().UTC()
		}
	}

	m := Message{
		ID:           msg.ID,
		Channel:      msg.Channel,
		SenderID:     msg.SenderID,
		Sender:       msg.Sender,
		Content:      msg.Content,
		TargetID:     msg.TargetID,
		TargetName:   msg.TargetName,
		TimestampUTC: ts,
		Source:       "push",
	}
	if _, err := ing.store.Ingest(m); err != nil {
		ing.logger.Printf("push ingest error: %v", err)
	}
}
