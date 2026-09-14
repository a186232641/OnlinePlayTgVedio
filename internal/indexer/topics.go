package indexer

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/gotd/td/tg"

	"github.com/hanfeilong/onlineplaytgvideo/internal/db"
)

// topicPageSize is the per-call limit for messages.getForumTopics. The server
// is free to return fewer than asked.
const topicPageSize = 100

// RefreshTopics enumerates a forum megagroup's topics and upserts one child
// channel row per topic (dialog_kind='topic', parent_channel_id + topic_id).
//
// Topic rows deliberately copy the PARENT's tg_channel_id and access_hash: the
// topic is not a separate Telegram peer, it's a message thread inside the
// group. Keeping the parent's peer fields means every existing code path that
// builds an InputPeer or re-fetches a message (refresh.go, the cache
// downloader) works on a topic row without knowing topics exist.
func (i *Indexer) RefreshTopics(ctx context.Context, channelID, userID int64) (int, error) {
	ch, err := i.db.ChannelByID(ctx, channelID, userID)
	if err != nil {
		return 0, err
	}
	if ch.DialogKind == db.DialogKindTopic {
		return 0, fmt.Errorf("频道 %d 本身就是一个话题,不能再枚举话题", channelID)
	}
	cli, err := i.tgmgr.ClientForSession(ch.TGSessionID)
	if err != nil {
		return 0, fmt.Errorf("tg client: %w", err)
	}
	return i.discoverTopics(ctx, cli.API, ch)
}

func (i *Indexer) discoverTopics(ctx context.Context, api *tg.Client, ch *db.Channel) (int, error) {
	peer := &tg.InputPeerChannel{ChannelID: ch.TGChannelID, AccessHash: ch.AccessHash}

	var (
		offsetDate, offsetID, offsetTopic int
		found                             int
	)
	for {
		select {
		case <-ctx.Done():
			return found, ctx.Err()
		default:
		}

		resp, err := api.MessagesGetForumTopics(ctx, &tg.MessagesGetForumTopicsRequest{
			Peer:        peer,
			OffsetDate:  offsetDate,
			OffsetID:    offsetID,
			OffsetTopic: offsetTopic,
			Limit:       topicPageSize,
		})
		if err != nil {
			return found, err
		}
		if len(resp.Topics) == 0 {
			break
		}

		// The topic list paginates on the *last message* of the last topic, so
		// we need those messages' dates.
		dateByMsgID := make(map[int]int, len(resp.Messages))
		for _, mc := range resp.Messages {
			if m, ok := mc.(*tg.Message); ok {
				dateByMsgID[m.ID] = m.Date
			}
		}

		lastTopic := offsetTopic
		for _, tc := range resp.Topics {
			t, ok := tc.(*tg.ForumTopic)
			if !ok {
				continue // forumTopicDeleted
			}
			if err := i.upsertTopic(ctx, ch, t); err != nil {
				return found, err
			}
			found++
			lastTopic = t.ID
			offsetID = t.TopMessage
			if d, ok := dateByMsgID[t.TopMessage]; ok {
				offsetDate = d
			}
		}
		// Only two things end the walk: an empty page, or a cursor that didn't
		// advance (which would otherwise loop forever). A short page must NOT:
		// getForumTopics explicitly lets the server return fewer topics than the
		// limit, so treating that as "the end" silently truncates the topic list.
		if lastTopic == offsetTopic {
			break
		}
		offsetTopic = lastTopic
	}

	if err := i.db.SetTopicsSynced(ctx, ch.ID); err != nil {
		return found, err
	}
	slog.Info("topics discovered", "channel_id", ch.ID, "title", ch.Title, "topics", found)
	return found, nil
}

func (i *Indexer) upsertTopic(ctx context.Context, parent *db.Channel, t *tg.ForumTopic) error {
	parentID := parent.ID
	topicID := int32(t.ID)
	row := &db.Channel{
		UserID:          parent.UserID,
		TGSessionID:     parent.TGSessionID,
		TGChannelID:     parent.TGChannelID,
		AccessHash:      parent.AccessHash,
		Title:           t.Title,
		DialogKind:      db.DialogKindTopic,
		ParentChannelID: &parentID,
		TopicID:         &topicID,
		TopicClosed:     t.Closed,
	}
	if t.IconColor != 0 {
		c := int32(t.IconColor)
		row.TopicIconColor = &c
	}
	if id, ok := t.GetIconEmojiID(); ok && id != 0 {
		row.TopicIconEmojiID = &id
	}
	if _, err := i.db.UpsertChannel(ctx, row); err != nil {
		return fmt.Errorf("upsert topic %d (%s): %w", t.ID, t.Title, err)
	}
	return nil
}

// forumProbeTimeout bounds the one RPC that asks whether a peer is a forum.
const forumProbeTimeout = 30 * time.Second

// refreshForumFlag re-checks with Telegram whether this channel is a forum and
// persists the answer, returning the current truth.
//
// Why sync re-checks something discovery already set: the flag only gets
// written when dialogs are enumerated, so any row created before forum support
// existed still says false — and a plain sync of a forum group is actively
// wrong, not merely incomplete. messages.getHistory on a forum returns every
// topic's messages flattened into the group row, so all the media piles into
// one undifferentiated bucket and the topic structure is lost. One cheap RPC
// per sync run is well worth avoiding that.
//
// A failed probe is not fatal: it keeps whatever the row already said.
func (i *Indexer) refreshForumFlag(ctx context.Context, api *tg.Client, ch *db.Channel) bool {
	if ch.DialogKind == db.DialogKindTopic {
		return false
	}
	probeCtx, cancel := context.WithTimeout(ctx, forumProbeTimeout)
	defer cancel()

	chats, err := api.ChannelsGetChannels(probeCtx, []tg.InputChannelClass{
		&tg.InputChannel{ChannelID: ch.TGChannelID, AccessHash: ch.AccessHash},
	})
	if err != nil {
		slog.Warn("forum probe failed, keeping stored flag",
			"channel_id", ch.ID, "is_forum", ch.IsForum, "err", err)
		return ch.IsForum
	}
	for _, c := range chats.GetChats() {
		full, ok := c.(*tg.Channel)
		if !ok || full.ID != ch.TGChannelID {
			continue
		}
		if full.Forum != ch.IsForum {
			slog.Info("forum flag corrected", "channel_id", ch.ID, "title", ch.Title,
				"was", ch.IsForum, "now", full.Forum)
			if err := i.db.SetIsForum(ctx, ch.ID, full.Forum); err != nil {
				slog.Warn("persist forum flag", "channel_id", ch.ID, "err", err)
			}
			ch.IsForum = full.Forum
		}
		return full.Forum
	}
	return ch.IsForum
}
