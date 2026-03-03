package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/H-strangeone/lan-suite/internal/api"
	"github.com/H-strangeone/lan-suite/internal/ccn"
	"github.com/H-strangeone/lan-suite/internal/chat"
	"github.com/H-strangeone/lan-suite/internal/config"
	"github.com/H-strangeone/lan-suite/internal/crypto"
	"github.com/H-strangeone/lan-suite/internal/discovery"
	"github.com/H-strangeone/lan-suite/internal/identity"
	"github.com/H-strangeone/lan-suite/internal/storage"
	"github.com/H-strangeone/lan-suite/internal/transport"
)

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	fmt.Println(`
  ██╗      █████╗ ███╗   ██╗    ███████╗██╗   ██╗██╗████████╗███████╗
  ██║     ██╔══██╗████╗  ██║    ██╔════╝██║   ██║██║╚══██╔══╝██╔════╝
  ██║     ███████║██╔██╗ ██║    ███████╗██║   ██║██║   ██║   █████╗
  ██║     ██╔══██║██║╚██╗██║    ╚════██║██║   ██║██║   ██║   ██╔══╝
  ███████╗██║  ██║██║ ╚████║    ███████║╚██████╔╝██║   ██║   ███████╗hehehe
  ╚══════╝╚═╝  ╚═╝╚═╝  ╚═══╝   ╚══════╝ ╚═════╝ ╚═╝   ╚═╝   ╚══════╝
  v0.1.0 — Phase 1: Foundation
`)

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("[boot] config: %v", err)
	}
	log.Printf("[boot] env=%s http=%s", cfg.Env, cfg.Addr())

	nodeIdentity, err := crypto.LoadOrCreate(cfg.DataDir)
	if err != nil {
		log.Fatalf("[boot] identity: %v", err)
	}
	log.Printf("[boot] nodeID=%s...", nodeIdentity.NodeID[:16])
	store, err := storage.New(cfg, nodeIdentity.PrivateKey())
	if err != nil {
		log.Fatalf("[boot] storage: %v", err)
	}

	jwtSecret := cfg.JWTSecret
	if jwtSecret == "" && cfg.IsDev() {
		jwtSecret = "dev-secret-do-not-use-in-production-at-all"
		log.Println("[boot] WARNING: using dev JWT secret")
	}
	jwtMgr := identity.NewManager(jwtSecret, cfg.JWTExpiryHrs, nodeIdentity.NodeID)

	hub := transport.NewHub()
	go hub.Run()
	log.Println("[boot] websocket hub running")

	ccnRouter := ccn.NewRouter(
		nodeIdentity.NodeID,
		int64(cfg.MaxStoreMB)*1024*1024,
		func(face ccn.Face, pkt *ccn.Packet) {
			log.Printf("[ccn] → face=%s type=%s", face, pkt.Type)
		},
	)
	ccnRouter.AddRoute(ccn.Name{"chat"}, ccn.FaceMulticast)
	ccnRouter.AddRoute(ccn.Name{"file"}, ccn.FaceMulticast)
	ccnRouter.AddRoute(ccn.Name{"video"}, ccn.FaceMulticast)
	log.Println("[boot] CCN router running")

	chatMgr, err := chat.New(store, hub, cfg.DataDir)
	if err != nil {
		log.Fatalf("[boot] chat: %v", err)
	}
	hub.SetChatHandler(chatMgr)
	hub.SetRoomRegistry(chatMgr)
	log.Println("[boot] chat manager running")

	disc := discovery.New(cfg, nodeIdentity, ccnRouter, hub)
	if err := disc.Start(); err != nil {
		log.Printf("[boot] discovery unavailable: %v (non-fatal)", err)
	} else {
		log.Printf("[boot] discovery on %s", cfg.MulticastAddr())
	}

	root := http.NewServeMux()

	allowedOrigins := make(map[string]bool, len(cfg.AllowedOrigins))
	for _, o := range cfg.AllowedOrigins {
		allowedOrigins[o] = true
	}

	root.Handle(
		"/ws",
		transport.ServeWS(hub, jwtMgr, allowedOrigins),
	)

	apiMux := http.NewServeMux()

	apiMux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		clients, rooms := hub.Stats()
		peers := disc.Peers()
		cs := ccnRouter.ContentStoreStats()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(
			w,
			`{"status":"ok","node":"%s","ws_clients":%d,"ws_rooms":%d,"lan_peers":%d,"cs_entries":%d}`,
			nodeIdentity.NodeID[:8],
			clients,
			rooms,
			len(peers),
			cs.EntryCount,
		)
	})

	apiMux.HandleFunc("GET /api/discovery/announce", func(w http.ResponseWriter, r *http.Request) {
		ann := disc.SelfAnnouncement()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ann)
	})
	apiMux.HandleFunc("POST /api/discovery/announce", func(w http.ResponseWriter, r *http.Request) {
		var ann discovery.Announcement
		if err := json.NewDecoder(r.Body).Decode(&ann); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		ip, _, _ := net.SplitHostPort(r.RemoteAddr)
		disc.HandleRemoteAnnouncement(ann, net.ParseIP(ip))
		w.WriteHeader(http.StatusOK)
	})

	authMW := api.Auth(jwtMgr)

	apiMux.Handle(
		"GET /api/peers",
		authMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			type peerJSON struct {
				NodeID      string   `json:"node_id"`
				DisplayName string   `json:"display_name"`
				Services    []string `json:"services"`
				IP          string   `json:"ip"`
				HTTPPort    int      `json:"http_port"`
				Online      bool     `json:"online"`
			}

			peers := disc.Peers()
			result := make([]peerJSON, 0, len(peers))
			for _, p := range peers {
				result = append(result, peerJSON{
					NodeID:      p.NodeID,
					DisplayName: p.DisplayName,
					Services:    p.Services,
					IP:          p.IP.String(),
					HTTPPort:    p.HTTPPort,
					Online:      p.Online,
				})
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(result)
		})),
	)

	apiMux.Handle(
		"POST /api/auth",
		api.AuthHandler(jwtMgr),
	)

	chatHandler := api.NewChatHandler(chatMgr)
	apiMux.Handle("/api/chat/", authMW(chatHandler))

	apiMux.Handle("GET /api/rooms", authMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		counts := hub.RoomMemberCounts()
		data, err := chatMgr.RoomListJSON(counts)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	})))

	rl := api.NewRateLimiter(cfg.HTTPRatePerMin)

	httpHandler := api.Chain(
		apiMux,
		api.Recovery,
		api.Logger,
		api.SecureHeaders(cfg),
		api.CORS(cfg),
		rl.Middleware,
	)

	root.Handle("/", httpHandler)

	server := &http.Server{
		Addr:         cfg.Addr(),
		Handler:      root,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		log.Printf("[boot] HTTP listening on %s", cfg.Addr())
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		log.Fatalf("[shutdown] server error: %v", err)
	case sig := <-quit:
		log.Printf("[shutdown] signal=%v", sig)
	}

	disc.Stop()
	ccnRouter.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	server.Shutdown(ctx)

	log.Println("[shutdown] clean exit")
}
