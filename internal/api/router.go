package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/hanfeilong/onlineplaytgvideo/internal/api/handlers"
	"github.com/hanfeilong/onlineplaytgvideo/internal/auth/web"
	"github.com/hanfeilong/onlineplaytgvideo/internal/config"
	"github.com/hanfeilong/onlineplaytgvideo/internal/db"
	"github.com/hanfeilong/onlineplaytgvideo/internal/httpx"
	"github.com/hanfeilong/onlineplaytgvideo/internal/indexer"
	"github.com/hanfeilong/onlineplaytgvideo/internal/tglogin"
	"github.com/hanfeilong/onlineplaytgvideo/internal/tgmanager"
)

type Deps struct {
	Cfg           *config.Config
	DB            *db.DB
	Login         *tglogin.Manager
	Indexer       *indexer.Indexer
	TGMgr         *tgmanager.Manager
	OnFavAdd      func(userID, videoID int64)
	OnFavRemove   func(userID, videoID int64)
	StreamHandler http.HandlerFunc
}

func NewRouter(d Deps) http.Handler {
	cfg, database := d.Cfg, d.DB
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	})

	authH := &handlers.AuthHandlers{Cfg: cfg, DB: database}
	tgH := &handlers.TGLoginHandlers{DB: database, Login: d.Login, TGMgr: d.TGMgr, Indexer: d.Indexer}
	chH := &handlers.ChannelsHandlers{DB: database, Indexer: d.Indexer}
	vidH := &handlers.VideosHandlers{Cfg: cfg, DB: database}
	favH := &handlers.FavoritesHandlers{DB: database, OnAdd: d.OnFavAdd, OnRemove: d.OnFavRemove}

	r.Route("/api", func(r chi.Router) {
		r.Route("/auth", func(r chi.Router) {
			r.Post("/register", authH.Register)
			r.Post("/login", authH.Login)
			r.Post("/logout", authH.Logout)

			r.Group(func(r chi.Router) {
				r.Use(web.RequireUser(cfg.JWTSecret))
				r.Get("/me", authH.Me)
			})
		})

		r.Group(func(r chi.Router) {
			r.Use(web.RequireUser(cfg.JWTSecret))

			r.Route("/tg", func(r chi.Router) {
				r.Post("/login/start", tgH.Start)
				r.Post("/login/code", tgH.Code)
				r.Post("/login/password", tgH.Password)

				r.Route("/sessions", func(r chi.Router) {
					r.Get("/", tgH.ListSessions)
					r.Patch("/{id}", tgH.UpdateLabel)
					r.Delete("/{id}", tgH.DeleteSession)
					r.Post("/{id}/refresh", tgH.RefreshDiscover)
				})
			})

			r.Route("/channels", func(r chi.Router) {
				r.Get("/", chH.List)
				r.Get("/{id}/videos", chH.ChannelVideos)
				r.Post("/{id}/index", chH.EnableIndex)
				r.Delete("/{id}/index", chH.DisableIndex)
				r.Post("/{id}/import", chH.Import)
			})

			r.Route("/videos", func(r chi.Router) {
				r.Get("/search", vidH.Search)
				r.Get("/{id}", vidH.Get)
				r.Get("/{id}/thumb", vidH.Thumb)
				if d.StreamHandler != nil {
					r.Get("/{id}/stream", d.StreamHandler)
				}
			})

			r.Route("/favorites", func(r chi.Router) {
				r.Get("/", favH.List)
				r.Post("/", favH.Add)
				r.Delete("/{video_id}", favH.Remove)
			})
		})
	})

	return r
}
