package video

import (
	"context"
	"errors"
	"fmt"

	"github.com/gotd/td/tg"

	"github.com/hanfeilong/onlineplaytgvideo/internal/db"
	"github.com/hanfeilong/onlineplaytgvideo/internal/tgmanager"
)

// RefreshFileReference re-fetches the original message via the same TG
// session that owns the channel, extracts the fresh Document.FileReference,
// persists it, and updates the supplied video pointer in place.
func RefreshFileReference(ctx context.Context, database *db.DB, mgr *tgmanager.Manager, v *db.Video) error {
	ch, err := database.ChannelByID(ctx, v.ChannelID, v.UserID)
	if err != nil {
		return err
	}
	cli, err := mgr.ClientForSession(ch.TGSessionID)
	if err != nil {
		return err
	}
	resp, err := cli.API.ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{
		Channel: &tg.InputChannel{
			ChannelID:  ch.TGChannelID,
			AccessHash: ch.AccessHash,
		},
		ID: []tg.InputMessageClass{&tg.InputMessageID{ID: int(v.TGMessageID)}},
	})
	if err != nil {
		return fmt.Errorf("channels.getMessages: %w", err)
	}

	var msgs []tg.MessageClass
	switch r := resp.(type) {
	case *tg.MessagesMessages:
		msgs = r.Messages
	case *tg.MessagesMessagesSlice:
		msgs = r.Messages
	case *tg.MessagesChannelMessages:
		msgs = r.Messages
	default:
		return errors.New("unexpected messages response")
	}

	for _, mc := range msgs {
		msg, ok := mc.(*tg.Message)
		if !ok {
			continue
		}
		media, ok := msg.Media.(*tg.MessageMediaDocument)
		if !ok {
			continue
		}
		doc, ok := media.Document.AsNotEmpty()
		if !ok {
			continue
		}
		// JSON-imported placeholder rows have TGDocID=0; in that case accept
		// the first non-empty doc on the message. Otherwise require an exact
		// match to handle messages with multiple media.
		if v.TGDocID != 0 && doc.ID != v.TGDocID {
			continue
		}
		v.TGDocID = doc.ID
		v.AccessHash = doc.AccessHash
		v.FileReference = doc.FileReference
		if v.SizeBytes == 0 {
			v.SizeBytes = doc.Size
		}
		if v.Mime == "" {
			v.Mime = doc.MimeType
		}
		if err := database.UpdateVideoLocator(ctx, v.ID, doc.ID, doc.AccessHash, doc.FileReference); err != nil {
			return err
		}
		return nil
	}
	return errors.New("video document not found in refreshed message")
}
