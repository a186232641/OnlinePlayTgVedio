package video

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/gotd/td/tg"

	"github.com/hanfeilong/onlineplaytgvideo/internal/db"
	"github.com/hanfeilong/onlineplaytgvideo/internal/tgmanager"
	"github.com/hanfeilong/onlineplaytgvideo/internal/tgmedia"
)

// resolveTimeout bounds the channels.getMessages call that resolves a video's
// fresh file locator. A stuck TG connection otherwise hangs this until the
// browser disconnects, so the video never starts.
const resolveTimeout = 20 * time.Second

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

	// getMessages goes to the channel's DC; like block fetches, the first hit
	// on a not-yet-connected DC can time out on a flaky link, so retry.
	var msgs []tg.MessageClass
	for attempt := 0; attempt < fetchAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(retryBackoff):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		fetchCtx, cancel := context.WithTimeout(ctx, resolveTimeout)
		msgs, err = fetchMessages(fetchCtx, cli.API, ch, int(v.TGMsgID))
		cancel()
		if err == nil {
			break
		}
		if !transient(err) {
			return err
		}
		slog.Warn("resolve retry", "video_id", v.ID, "tg_msg_id", v.TGMsgID, "attempt", attempt+1, "err", err)
	}
	if err != nil {
		return err
	}
	if len(msgs) == 0 {
		return fmt.Errorf("消息 %d 在 TG 上找不到 (可能已删除/你已退出频道/access_hash 过期)", v.TGMsgID)
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
		v.DCID = doc.DCID
		v.ThumbSize = tgmedia.PickDocThumb(doc.Thumbs)
		if v.FileSize == 0 {
			v.FileSize = doc.Size
		}
		if v.MimeType == "" {
			v.MimeType = doc.MimeType
		}
		if err := database.UpdateVideoLocator(ctx, v.ID, doc.ID, doc.AccessHash, doc.FileReference, doc.Size, doc.MimeType, doc.DCID, v.ThumbSize); err != nil {
			return fmt.Errorf("update locator: %w", err)
		}
		return nil
	}
	return fmt.Errorf("消息 %d 不含视频 (可能消息只有文本/图片,或 caption 与原视频是两条不同消息)", v.TGMsgID)
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

// RefreshPhotoReference is RefreshFileReference for images: it re-fetches the
// message, pulls the fresh photo id / access_hash / file_reference plus the
// size ladder, persists them and updates p in place.
//
// Images need this for the same two reasons videos do: JSON-imported rows have
// no locator at all until first view, and Telegram's file_reference expires
// (FILE_REFERENCE_EXPIRED) after a while.
func RefreshPhotoReference(ctx context.Context, database *db.DB, mgr *tgmanager.Manager, p *db.Photo) error {
	ch, err := database.ChannelByID(ctx, p.ChannelID, p.UserID)
	if err != nil {
		return fmt.Errorf("channel lookup: %w", err)
	}
	cli, err := mgr.ClientForSession(ch.TGSessionID)
	if err != nil {
		return fmt.Errorf("tg client: %w", err)
	}

	var msgs []tg.MessageClass
	for attempt := 0; attempt < fetchAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(retryBackoff):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		fetchCtx, cancel := context.WithTimeout(ctx, resolveTimeout)
		msgs, err = fetchMessages(fetchCtx, cli.API, ch, int(p.TGMsgID))
		cancel()
		if err == nil {
			break
		}
		if !transient(err) {
			return err
		}
		slog.Warn("photo resolve retry", "photo_id", p.ID, "tg_msg_id", p.TGMsgID, "attempt", attempt+1, "err", err)
	}
	if err != nil {
		return err
	}
	if len(msgs) == 0 {
		return fmt.Errorf("消息 %d 在 TG 上找不到 (可能已删除/你已退出频道/access_hash 过期)", p.TGMsgID)
	}

	for _, mc := range msgs {
		msg, ok := mc.(*tg.Message)
		if !ok {
			continue
		}
		media, ok := msg.Media.(*tg.MessageMediaPhoto)
		if !ok {
			continue
		}
		pc, ok := media.GetPhoto()
		if !ok {
			continue
		}
		photo, ok := pc.AsNotEmpty()
		if !ok {
			continue
		}
		full, thumb := tgmedia.PickPhotoSizes(photo.Sizes)
		if full.Type == "" {
			return fmt.Errorf("消息 %d 的图片没有可下载的尺寸", p.TGMsgID)
		}
		p.TGPhotoID = photo.ID
		p.AccessHash = photo.AccessHash
		p.FileReference = photo.FileReference
		p.DCID = photo.DCID
		p.SizeType = full.Type
		p.ThumbSize = thumb.Type
		if full.Bytes > 0 {
			p.FileSize = full.Bytes
		}
		if err := database.UpdatePhotoLocator(ctx, p.ID, photo.ID, photo.AccessHash, photo.FileReference,
			photo.DCID, full.Bytes, full.Type, thumb.Type); err != nil {
			return fmt.Errorf("update photo locator: %w", err)
		}
		return nil
	}
	return fmt.Errorf("消息 %d 不含图片 (可能消息已被编辑,或图片与文本是两条不同消息)", p.TGMsgID)
}
