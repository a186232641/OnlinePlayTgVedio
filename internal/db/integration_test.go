package db

import (
	"context"
	"os"
	"testing"
	"time"
)

// Integration coverage for the parts of the schema that unit tests can't reach:
// the merged video+image pagination, the forum-topic rows, the (kind, id) cache
// keys and the favorites/pin bookkeeping across two tables.
//
// It is skipped unless TEST_DB_DSN points at a THROWAWAY Postgres — it wipes
// every table before running. `make test` therefore stays pure-unit.
//
//	createdb tgvtest
//	TEST_DB_DSN='postgres:///tgvtest?sslmode=disable' go test ./internal/db/ -run TestIntegration
func dsn(t *testing.T) string {
	s := os.Getenv("TEST_DB_DSN")
	if s == "" {
		t.Skip("TEST_DB_DSN not set — integration test skipped")
	}
	return s
}

func TestIntegration(t *testing.T) {
	ctx := context.Background()
	d, err := Open(ctx, dsn(t))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.Migrate(ctx); err != nil {
		t.Fatal("migrate:", err)
	}

	// clean slate
	for _, q := range []string{
		"DELETE FROM photo_favorites", "DELETE FROM favorites", "DELETE FROM cache_entries",
		"DELETE FROM photos", "DELETE FROM videos", "DELETE FROM channels",
		"DELETE FROM tg_sessions", "DELETE FROM users",
	} {
		if _, err := d.Exec(ctx, q); err != nil {
			t.Fatal(q, err)
		}
	}

	var uid int64
	if err := d.QueryRow(ctx, `INSERT INTO users (email, password_hash) VALUES ('a@b.c','x') RETURNING id`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	var sid int64
	if err := d.QueryRow(ctx, `INSERT INTO tg_sessions (user_id, status) VALUES ($1,'active') RETURNING id`, uid).Scan(&sid); err != nil {
		t.Fatal(err)
	}

	// --- channels: a forum group + one topic -----------------------------
	forumID, err := d.UpsertChannel(ctx, &Channel{
		UserID: uid, TGSessionID: sid, TGChannelID: 111, AccessHash: 222,
		Title: "群组", DialogKind: DialogKindMegagroup, IsForum: true,
	})
	if err != nil {
		t.Fatal("upsert forum:", err)
	}
	topicNo := int32(7)
	color := int32(0x6FB9F0)
	emoji := int64(99)
	topicID, err := d.UpsertChannel(ctx, &Channel{
		UserID: uid, TGSessionID: sid, TGChannelID: 111, AccessHash: 222,
		Title: "话题A", DialogKind: DialogKindTopic,
		ParentChannelID: &forumID, TopicID: &topicNo,
		TopicIconColor: &color, TopicIconEmojiID: &emoji, TopicClosed: true,
	})
	if err != nil {
		t.Fatal("upsert topic:", err)
	}
	if topicID == forumID {
		t.Fatal("topic collided with its parent row")
	}
	if got, err := d.ChannelByID(ctx, topicID, uid); err != nil || !got.TopicClosed ||
		got.TopicIconColor == nil || *got.TopicIconColor != color ||
		got.TopicIconEmojiID == nil || *got.TopicIconEmojiID != emoji {
		t.Fatalf("topic metadata not persisted: %+v %v", got, err)
	}
	// A re-discovery of the same topic updates in place rather than inserting.
	if again, err := d.UpsertChannel(ctx, &Channel{
		UserID: uid, TGSessionID: sid, TGChannelID: 111, AccessHash: 222,
		Title: "话题A改名", DialogKind: DialogKindTopic,
		ParentChannelID: &forumID, TopicID: &topicNo, TopicClosed: true,
	}); err != nil || again != topicID {
		t.Fatalf("topic upsert not idempotent: id=%d err=%v", again, err)
	}
	if err := d.SetTopicsSynced(ctx, forumID); err != nil {
		t.Fatal(err)
	}
	topics, err := d.ListTopics(ctx, forumID, uid)
	if err != nil || len(topics) != 1 {
		t.Fatalf("ListTopics = %d rows, err=%v", len(topics), err)
	}
	if topics[0].TopicID == nil || *topics[0].TopicID != 7 || !topics[0].TopicClosed {
		t.Fatalf("topic row scanned wrong: %+v", topics[0])
	}
	if n, err := d.CountTopics(ctx, forumID, uid); err != nil || n != 1 {
		t.Fatalf("CountTopics = %d, %v", n, err)
	}
	stats, err := d.TopicStats(ctx, uid)
	if err != nil || stats[forumID].Topics != 1 {
		t.Fatalf("TopicStats = %v, %v", stats, err)
	}
	// A forum whose topics hold nothing must report zero media — that is what
	// keeps never-synced groups out of the browsing view.
	if st := stats[forumID]; st.Videos != 0 || st.Photos != 0 {
		t.Fatalf("unsynced forum reports media: %+v", st)
	}
	if ch, err := d.ChannelByID(ctx, forumID, uid); err != nil || !ch.IsForum || ch.TopicsSyncedAt == nil {
		t.Fatalf("forum row: %+v %v", ch, err)
	}

	// --- media: interleaved videos and photos in the topic ---------------
	base := time.Date(2024, 5, 1, 12, 0, 0, 0, time.UTC)
	at := func(i int) *time.Time { t := base.Add(time.Duration(i) * time.Hour); return &t }

	for i := 1; i <= 5; i++ {
		if _, err := d.UpsertVideo(ctx, &Video{
			UserID: uid, ChannelID: topicID, TGMsgID: int64(100 + i*2),
			Date: at(i * 2), FileName: "anchor-2024-05-0" + string(rune('0'+i)) + " 10:00:00.mp4",
			FileSize: 1000, DurationSeconds: 60, MediaType: "video_file",
			TGDocID: int64(9000 + i), AccessHash: 1, FileReference: []byte{1}, DCID: 2,
			ThumbSize: "m", Text: "视频说明",
		}); err != nil {
			t.Fatal("upsert video:", err)
		}
		if _, err := d.UpsertPhoto(ctx, &Photo{
			UserID: uid, ChannelID: topicID, TGMsgID: int64(101 + i*2),
			Date: at(i*2 + 1), FileSize: 500, Width: 1280, Height: 720,
			TGPhotoID: int64(7000 + i), AccessHash: 2, FileReference: []byte{2}, DCID: 4,
			SizeType: "y", ThumbSize: "m", Text: "图片说明",
		}); err != nil {
			t.Fatal("upsert photo:", err)
		}
	}

	if n, err := d.CountVideosByChannel(ctx, uid, topicID); err != nil || n != 5 {
		t.Fatalf("video count = %d, %v", n, err)
	}
	if n, err := d.CountPhotosByChannel(ctx, uid, topicID); err != nil || n != 5 {
		t.Fatalf("photo count = %d, %v", n, err)
	}
	if err := d.MarkChannelIndexed(ctx, topicID); err != nil {
		t.Fatal(err)
	}
	if ch, _ := d.ChannelByID(ctx, topicID, uid); ch.VideoCount != 5 || ch.PhotoCount != 5 {
		t.Fatalf("counts not recomputed: %d/%d", ch.VideoCount, ch.PhotoCount)
	}

	if st, _ := d.TopicStats(ctx, uid); st[forumID].Videos != 5 || st[forumID].Photos != 5 {
		t.Fatalf("topic media not aggregated onto the group: %+v", st[forumID])
	}

	// Source lookup: a topic resolves to itself plus its parent group; ids that
	// belong to someone else (or don't exist) are simply absent.
	srcs, err := d.ChannelSources(ctx, uid, []int64{topicID, forumID, 999999})
	if err != nil {
		t.Fatal("ChannelSources:", err)
	}
	if got := srcs[topicID]; got.ParentID != forumID || got.ParentTitle != "群组" || got.DialogKind != DialogKindTopic {
		t.Fatalf("topic source = %+v", got)
	}
	if got := srcs[forumID]; got.ParentID != 0 || got.Title != "群组" {
		t.Fatalf("group source = %+v", got)
	}
	if _, ok := srcs[999999]; ok || len(srcs) != 2 {
		t.Fatalf("unexpected sources: %+v", srcs)
	}
	if empty, err := d.ChannelSources(ctx, uid, nil); err != nil || len(empty) != 0 {
		t.Fatalf("empty lookup = %v, %v", empty, err)
	}

	// sync cursors must span both tables
	if max, err := d.MaxMsgIDForChannel(ctx, topicID, uid); err != nil || max != 111 {
		t.Fatalf("MaxMsgIDForChannel = %d, %v (want 111, the newest photo)", max, err)
	}
	if min, err := d.MinMsgIDForChannel(ctx, topicID, uid); err != nil || min != 102 {
		t.Fatalf("MinMsgIDForChannel = %d, %v (want 102, the oldest video)", min, err)
	}

	// --- merged paging: every row exactly once, newest first -------------
	walk := func(order string, limit int, kind string) []string {
		var seen []string
		cur := MediaCursor{}
		for page := 0; page < 50; page++ {
			items, next, more, err := d.ListMedia(ctx, ListMediaOpts{
				UserID: uid, ChannelID: topicID, Limit: limit, Cursor: cur, OrderBy: order, Kind: kind,
			})
			if err != nil {
				t.Fatal("ListMedia:", err)
			}
			for _, it := range items {
				seen = append(seen, it.Kind+":"+itoa(int(it.id())))
			}
			if !more {
				return seen
			}
			if next == cur && len(items) == 0 {
				t.Fatal("cursor stalled with an empty page")
			}
			cur = next
		}
		t.Fatal("pagination did not terminate")
		return nil
	}

	for _, limit := range []int{1, 2, 3, 7, 100} {
		got := walk("", limit, "")
		if len(got) != 10 {
			t.Fatalf("limit=%d: merged walk returned %d rows (want 10): %v", limit, len(got), got)
		}
		dedup := map[string]bool{}
		for _, k := range got {
			if dedup[k] {
				t.Fatalf("limit=%d: duplicate row %s in %v", limit, k, got)
			}
			dedup[k] = true
		}
		// newest first: the last message (photo, msg 111) must lead.
		if got[0] != "photo:"+itoa(int(lastPhotoID(ctx, t, d))) {
			t.Fatalf("limit=%d: merged order wrong, head = %s", limit, got[0])
		}
	}
	if got := walk("date_asc", 3, ""); len(got) != 10 {
		t.Fatalf("date_asc walk = %d rows", len(got))
	}
	if got := walk("", 3, MediaKindPhoto); len(got) != 5 {
		t.Fatalf("kind=photo walk = %d rows: %v", len(got), got)
	}
	if got := walk("name_asc", 3, MediaKindVideo); len(got) != 5 {
		t.Fatalf("name_asc video walk = %d rows", len(got))
	}
	if got := walk("duration", 3, ""); len(got) != 5 {
		t.Fatalf("duration walk should be videos-only, got %d", len(got))
	}

	// streamer filter implies videos only
	items, _, _, err := d.ListMedia(ctx, ListMediaOpts{
		UserID: uid, ChannelID: topicID, StreamerFilter: true, Streamer: "anchor", Limit: 100,
	})
	if err != nil {
		t.Fatal("streamer ListMedia:", err)
	}
	if len(items) != 5 {
		t.Fatalf("streamer filter returned %d rows (want 5)", len(items))
	}

	// --- search across both tables ---------------------------------------
	from := base.Add(-time.Hour)
	to := base.Add(100 * time.Hour)
	items, _, _, err = d.ListMedia(ctx, ListMediaOpts{
		UserID: uid, Q: "说明", DateFrom: &from, DateTo: &to, Limit: 100,
	})
	if err != nil || len(items) != 10 {
		t.Fatalf("search = %d rows, %v", len(items), err)
	}
	items, _, _, err = d.ListMedia(ctx, ListMediaOpts{UserID: uid, Text: "图片", Limit: 100})
	if err != nil || len(items) != 5 {
		t.Fatalf("text search = %d rows, %v", len(items), err)
	}

	// --- favorites, merged -------------------------------------------------
	vid := firstVideoID(ctx, t, d)
	pid := lastPhotoID(ctx, t, d)
	if err := d.AddFavorite(ctx, uid, vid); err != nil {
		t.Fatal(err)
	}
	if err := d.AddFavorite(ctx, uid, vid); err != nil { // idempotent
		t.Fatal(err)
	}
	if err := d.AddPhotoFavorite(ctx, uid, pid); err != nil {
		t.Fatal(err)
	}
	if ok, err := d.IsPhotoFavorite(ctx, uid, pid); err != nil || !ok {
		t.Fatalf("IsPhotoFavorite = %v, %v", ok, err)
	}
	items, _, _, err = d.ListMedia(ctx, ListMediaOpts{UserID: uid, FavOnly: true, Limit: 100})
	if err != nil || len(items) != 2 {
		t.Fatalf("favorites = %d rows, %v", len(items), err)
	}

	// --- cache entries are keyed by (kind, id) ----------------------------
	// Same numeric id in both spaces must not collide.
	for _, kind := range []string{MediaKindVideo, MediaKindPhoto} {
		if err := d.UpsertCacheEntry(ctx, &CacheEntry{
			Kind: kind, TGDocID: 4242, FilePath: kind + "s/4242.bin", Bytes: 10, Completed: true,
		}); err != nil {
			t.Fatal("upsert cache:", err)
		}
	}
	all, err := d.AllCompletedCacheEntries(ctx)
	if err != nil || len(all) != 2 {
		t.Fatalf("cache inventory = %d rows, %v", len(all), err)
	}
	if c, err := d.GetCacheEntry(ctx, MediaKindPhoto, 4242); err != nil || c.Kind != MediaKindPhoto {
		t.Fatalf("GetCacheEntry kind mismatch: %+v %v", c, err)
	}
	if err := d.TouchCache(ctx, MediaKindPhoto, 4242); err != nil {
		t.Fatal(err)
	}
	if err := d.MarkCacheIncomplete(ctx, MediaKindPhoto, 4242); err != nil {
		t.Fatal(err)
	}
	if err := d.DeleteCacheEntry(ctx, MediaKindVideo, 4242); err != nil {
		t.Fatal(err)
	}

	// pin/unpin by row id, both kinds
	if err := d.UpsertCacheEntry(ctx, &CacheEntry{Kind: MediaKindPhoto, TGDocID: 7005, FilePath: "photos/7005.bin", Completed: true}); err != nil {
		t.Fatal(err)
	}
	if id, done, err := d.PinByPhotoID(ctx, pid); err != nil || id != 7005 || !done {
		t.Fatalf("PinByPhotoID = %d,%v,%v", id, done, err)
	}
	if err := d.UnpinPhotoIfNotFavorited(ctx, pid); err != nil {
		t.Fatal(err)
	}
	if c, _ := d.GetCacheEntry(ctx, MediaKindPhoto, 7005); !c.Pinned {
		t.Fatal("unpinned a photo that is still favorited")
	}
	if err := d.RemovePhotoFavorite(ctx, uid, pid); err != nil {
		t.Fatal(err)
	}
	if err := d.UnpinPhotoIfNotFavorited(ctx, pid); err != nil {
		t.Fatal(err)
	}
	if c, _ := d.GetCacheEntry(ctx, MediaKindPhoto, 7005); c.Pinned {
		t.Fatal("photo stayed pinned after the last favorite went away")
	}
	if _, _, err := d.PinByVideoID(ctx, vid); err != nil {
		t.Fatal("PinByVideoID:", err)
	}
	if err := d.UnpinIfNotFavorited(ctx, vid); err != nil {
		t.Fatal(err)
	}

	// --- locator + thumb updates ------------------------------------------
	if err := d.UpdateVideoLocator(ctx, vid, 12345, 6, []byte{9}, 777, "video/mp4", 5, "s"); err != nil {
		t.Fatal(err)
	}
	if v, _ := d.VideoByID(ctx, vid, uid); v.TGDocID != 12345 || v.ThumbSize != "s" || v.FileSize != 777 {
		t.Fatalf("video locator not persisted: %+v", v)
	}
	if err := d.SetVideoThumbPath(ctx, vid, "thumbs/video_1.jpg"); err != nil {
		t.Fatal(err)
	}
	if err := d.UpdatePhotoLocator(ctx, pid, 555, 6, []byte{9}, 3, 888, "x", "s"); err != nil {
		t.Fatal(err)
	}
	p, err := d.PhotoByID(ctx, pid, uid)
	if err != nil || p.TGPhotoID != 555 || p.SizeType != "x" || p.FileSize != 888 || p.DCID != 3 {
		t.Fatalf("photo locator not persisted: %+v %v", p, err)
	}
	if err := d.SetPhotoThumbPath(ctx, pid, "thumbs/photo_1.jpg"); err != nil {
		t.Fatal(err)
	}
	if p, _ := d.PhotoByID(ctx, pid, uid); p.ThumbPath != "thumbs/photo_1.jpg" {
		t.Fatalf("thumb path = %q", p.ThumbPath)
	}
	if _, err := d.PhotoByIDAny(ctx, pid); err != nil {
		t.Fatal("PhotoByIDAny:", err)
	}

	// --- album caption propagation, both tables ---------------------------
	if _, err := d.UpsertPhoto(ctx, &Photo{
		UserID: uid, ChannelID: topicID, TGMsgID: 900, Date: at(40),
		GroupedID: 4242, Text: "相册标题", TGPhotoID: 1, SizeType: "y",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertPhoto(ctx, &Photo{
		UserID: uid, ChannelID: topicID, TGMsgID: 901, Date: at(41),
		GroupedID: 4242, TGPhotoID: 2, SizeType: "y",
	}); err != nil {
		t.Fatal(err)
	}
	if err := d.PropagatePhotoGroupCaption(ctx, uid, topicID, 4242); err != nil {
		t.Fatal(err)
	}
	var silent string
	if err := d.QueryRow(ctx, `SELECT COALESCE(text,'') FROM photos WHERE user_id=$1 AND tg_msg_id=901`, uid).Scan(&silent); err != nil {
		t.Fatal(err)
	}
	if silent != "相册标题" {
		t.Fatalf("photo album caption not propagated: %q", silent)
	}

	// --- batched page writes -----------------------------------------------
	// The sync hot path writes a whole page per round trip; it must produce
	// exactly the same rows as the single-row writers, including the upsert
	// semantics for a page that overlaps what is already stored.
	var pageV []*Video
	var pageP []*Photo
	for i := 0; i < 20; i++ {
		pageV = append(pageV, &Video{
			UserID: uid, ChannelID: topicID, TGMsgID: int64(2000 + i), Date: at(50 + i),
			FileName: "batch.mp4", MediaType: "video_file", GroupedID: 555,
			TGDocID: int64(8000 + i), AccessHash: 1, FileReference: []byte{1},
		})
		pageP = append(pageP, &Photo{
			UserID: uid, ChannelID: topicID, TGMsgID: int64(3000 + i), Date: at(80 + i),
			GroupedID: 666, TGPhotoID: int64(6000 + i), SizeType: "y",
		})
	}
	pageV[3].Text = "批量相册标题"
	pageP[7].Text = "批量图片相册标题"
	if err := d.UpsertVideos(ctx, pageV); err != nil {
		t.Fatal("UpsertVideos:", err)
	}
	if err := d.UpsertPhotos(ctx, pageP); err != nil {
		t.Fatal("UpsertPhotos:", err)
	}
	// Re-writing the same page must be a no-op, not a duplicate.
	if err := d.UpsertVideos(ctx, pageV); err != nil {
		t.Fatal("UpsertVideos (repeat):", err)
	}
	if n, err := d.CountVideosByChannel(ctx, uid, topicID); err != nil || n != 25 {
		t.Fatalf("after batch: video count = %d, %v (want 25)", n, err)
	}
	if err := d.UpsertVideos(ctx, nil); err != nil {
		t.Fatal("empty batch must be a no-op:", err)
	}

	if err := d.PropagateCaptions(ctx, uid, topicID, []int64{555}, []int64{666}); err != nil {
		t.Fatal("PropagateCaptions:", err)
	}
	var vCap, pCap string
	if err := d.QueryRow(ctx, `SELECT COALESCE(text,'') FROM videos WHERE user_id=$1 AND tg_msg_id=2000`, uid).Scan(&vCap); err != nil {
		t.Fatal(err)
	}
	if err := d.QueryRow(ctx, `SELECT COALESCE(text,'') FROM photos WHERE user_id=$1 AND tg_msg_id=3000`, uid).Scan(&pCap); err != nil {
		t.Fatal(err)
	}
	if vCap != "批量相册标题" || pCap != "批量图片相册标题" {
		t.Fatalf("batched caption propagation failed: video=%q photo=%q", vCap, pCap)
	}
	if err := d.PropagateCaptions(ctx, uid, topicID, nil, nil); err != nil {
		t.Fatal("empty propagate must be a no-op:", err)
	}

	// --- bulk thumb-path clearing (cache GC) -------------------------------
	if err := d.SetPhotoThumbPath(ctx, pid, "thumbs/photo_1.jpg"); err != nil {
		t.Fatal(err)
	}
	if err := d.ClearThumbPaths(ctx, []int64{vid}, []int64{pid}); err != nil {
		t.Fatal("ClearThumbPaths:", err)
	}
	if p, _ := d.PhotoByID(ctx, pid, uid); p.ThumbPath != "" {
		t.Fatalf("thumb path not cleared: %q", p.ThumbPath)
	}
	if v, _ := d.VideoByID(ctx, vid, uid); v.ThumbPath != "" {
		t.Fatalf("video thumb path not cleared: %q", v.ThumbPath)
	}

	// --- clearing a channel wipes both tables ------------------------------
	nv, err := d.DeleteVideosByChannel(ctx, uid, topicID)
	if err != nil {
		t.Fatal(err)
	}
	np, err := d.DeletePhotosByChannel(ctx, uid, topicID)
	if err != nil {
		t.Fatal(err)
	}
	if nv != 25 || np != 27 {
		t.Fatalf("cleared %d videos / %d photos", nv, np)
	}
	if err := d.ResetHistoryComplete(ctx, topicID); err != nil {
		t.Fatal(err)
	}
}

func firstVideoID(ctx context.Context, t *testing.T, d *DB) int64 {
	t.Helper()
	var id int64
	if err := d.QueryRow(ctx, `SELECT id FROM videos ORDER BY id LIMIT 1`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func lastPhotoID(ctx context.Context, t *testing.T, d *DB) int64 {
	t.Helper()
	var id int64
	if err := d.QueryRow(ctx, `SELECT id FROM photos ORDER BY date DESC NULLS LAST, id DESC LIMIT 1`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
