package video

import (
	"context"
	"errors"
	"fmt"

	"github.com/gotd/td/tg"

	"github.com/hanfeilong/onlineplaytgvideo/internal/db"
	"github.com/hanfeilong/onlineplaytgvideo/internal/tgmanager"
)

// RefreshFileReference re-fetches the message that originally carried the
// video, extracts the fresh Document.FileReference, persists it, and updates
// the supplied video pointer in place. Returns nil if the refresh succeeded.
func RefreshFileReference(ctx context.Context, database *db.DB, mgr *tgmanager.Manager, v *db.Video) error {
	cli, err := mgr.ClientFor(v.UserID)
	if err != nil {
		return err
	}
	ch, err := database.ChannelByID(ctx, v.ChannelID, v.UserID)
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
		if doc.ID != v.TGDocID {
			continue
		}
		v.FileReference = doc.FileReference
		v.AccessHash = doc.AccessHash
		if err := database.UpdateVideoFileReference(ctx, v.ID, doc.FileReference); err != nil {
			return err
		}
		return nil
	}
	return errors.New("video document not found in refreshed message")
}
