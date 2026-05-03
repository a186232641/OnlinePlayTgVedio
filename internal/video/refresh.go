package video

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/gotd/td/tg"

	"github.com/hanfeilong/onlineplaytgvideo/internal/db"
	"github.com/hanfeilong/onlineplaytgvideo/internal/tgmanager"
)

// RefreshFileReference re-fetches the original message via the same TG
// session that owns the channel, extracts the fresh Document.FileReference,
// persists it, and updates the supplied video pointer in place.
//
// Dispatches based on dialog kind: channel/megagroup → channels.getMessages,
// group/user/anything else → messages.getMessages.
func RefreshFileReference(ctx context.Context, database *db.DB, mgr *tgmanager.Manager, v *db.Video) error {
	ch, err := database.ChannelByID(ctx, v.ChannelID, v.UserID)
	if err != nil {
		return fmt.Errorf("channel lookup: %w", err)
	}
	cli, err := mgr.ClientForSession(ch.TGSessionID)
	if err != nil {
		return fmt.Errorf("tg client: %w", err)
	}

	msgs, err := fetchMessages(ctx, cli.API, ch, int(v.TGMessageID))
	if err != nil {
		return err
	}
	if len(msgs) == 0 {
		return fmt.Errorf("消息 %d 在 TG 上找不到 (可能已删除/你已退出频道/access_hash 过期)", v.TGMessageID)
	}

	for _, mc := range msgs {
		msg, ok := mc.(*tg.Message)
		if !ok {
			slog.Debug("refresh skip non-message", "type", fmt.Sprintf("%T", mc))
			continue
		}
		media, ok := msg.Media.(*tg.MessageMediaDocument)
		if !ok {
			slog.Debug("refresh skip non-document", "media", fmt.Sprintf("%T", msg.Media))
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
		if err := database.UpdateVideoLocator(ctx, v.ID, doc.ID, doc.AccessHash, doc.FileReference, doc.Size, doc.MimeType); err != nil {
			return fmt.Errorf("update locator: %w", err)
		}
		return nil
	}
	return fmt.Errorf("消息 %d 不含视频 (可能消息只有文本/图片,或 caption 与原视频是两条不同消息)", v.TGMessageID)
}

func fetchMessages(ctx context.Context, api *tg.Client, ch *db.Channel, msgID int) ([]tg.MessageClass, error) {
	idList := []tg.InputMessageClass{&tg.InputMessageID{ID: msgID}}

	var resp tg.MessagesMessagesClass
	var err error
	switch ch.DialogKind {
	case db.DialogKindGroup, db.DialogKindUser:
		resp, err = api.MessagesGetMessages(ctx, idList)
	default:
		// channel / megagroup / forum (legacy rows)
		resp, err = api.ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{
			Channel: &tg.InputChannel{
				ChannelID:  ch.TGChannelID,
				AccessHash: ch.AccessHash,
			},
			ID: idList,
		})
	}
	if err != nil {
		return nil, fmt.Errorf("getMessages: %w", err)
	}

	switch r := resp.(type) {
	case *tg.MessagesMessages:
		return r.Messages, nil
	case *tg.MessagesMessagesSlice:
		return r.Messages, nil
	case *tg.MessagesChannelMessages:
		return r.Messages, nil
	default:
		return nil, errors.New("unexpected messages response type")
	}
}
