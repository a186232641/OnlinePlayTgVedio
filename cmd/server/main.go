package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hanfeilong/onlineplaytgvideo/internal/api"
	"github.com/hanfeilong/onlineplaytgvideo/internal/cache"
	"github.com/hanfeilong/onlineplaytgvideo/internal/config"
	"github.com/hanfeilong/onlineplaytgvideo/internal/db"
	"github.com/hanfeilong/onlineplaytgvideo/internal/indexer"
	"github.com/hanfeilong/onlineplaytgvideo/internal/tglogin"
	"github.com/hanfeilong/onlineplaytgvideo/internal/tgmanager"
	"github.com/hanfeilong/onlineplaytgvideo/internal/video"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cfg, err := config.Load()
	if err != nil {
		slog.Error("config load failed", "err", err)
		os.Exit(1)
	}

	rootCtx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	database, err := db.Open(rootCtx, cfg.DBDSN)
	if err != nil {
		slog.Error("db open failed", "err", err)
		os.Exit(1)
	}
	defer database.Close()

	if err := database.Migrate(rootCtx); err != nil {
		slog.Error("migrate failed", "err", err)
		os.Exit(1)
	}
	slog.Info("migrations applied")

	tgMgr := tgmanager.New(cfg, database)
	if err := tgMgr.RestoreActive(rootCtx); err != nil {
		slog.Warn("restore tg sessions", "err", err)
	}
	defer tgMgr.Shutdown()

	idx := indexer.New(cfg, database, tgMgr)
	idx.StartScheduler(rootCtx, cfg.SyncInterval)

	cacheMgr := cache.New(cfg, database, tgMgr)
	if err := cacheMgr.Start(rootCtx); err != nil {
		slog.Error("cache start failed", "err", err)
		os.Exit(1)
	}
	defer cacheMgr.Stop()

	stream := &video.StreamServer{Cfg: cfg, DB: database, TG: tgMgr, Cache: cacheMgr}

	loginMgr := tglogin.NewManager(cfg, database, func(uid, sid int64) {
		if err := tgMgr.Start(context.Background(), uid, sid); err != nil {
			slog.Warn("start tg client after login", "user_id", uid, "session_id", sid, "err", err)
			return
		}
		idx.TriggerDiscover(sid)
	})

	router := api.NewRouter(api.Deps{
		Cfg:           cfg,
		DB:            database,
		Login:         loginMgr,
		Indexer:       idx,
		TGMgr:         tgMgr,
		StreamHandler: stream.Handler(),
		OnFavAdd:      cacheMgr.EnqueueFavorite,
		OnFavRemove:   cacheMgr.HandleUnfavorite,
	})

	srv := &http.Server{
		Addr:              cfg.ServerAddr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		slog.Info("server listening", "addr", cfg.ServerAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", "err", err)
			cancel()
		}
	}()

	<-rootCtx.Done()
	slog.Info("shutting down")

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutCancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		slog.Error("shutdown error", "err", err)
	}
}
