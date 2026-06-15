package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/chenxi/slack-queue-bot/pkg/bot"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

func main() {
	dbPath := getEnv("DB_PATH", "./data/bot.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		log.Fatalf("create data dir: %v", err)
	}

	db, err := bot.NewDB(dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()
	log.Printf("database: %s", dbPath)

	api := slack.New(
		mustEnv("SLACK_BOT_TOKEN"),
		slack.OptionAppLevelToken(mustEnv("SLACK_APP_TOKEN")),
		slack.OptionLog(log.New(os.Stdout, "slack: ", log.LstdFlags)),
	)
	client := socketmode.New(
		api,
		socketmode.OptionLog(log.New(os.Stdout, "socketmode: ", log.LstdFlags)),
	)

	// ctx cancels on SIGINT/SIGTERM so the deferred db.Close runs cleanly
	// (flushes the SQLite WAL) before the pod is replaced.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// connected reflects the live Slack socket state for the k8s health probe.
	var connected atomic.Bool
	go serveHealth(getEnv("HEALTH_ADDR", ":8080"), &connected)

	go func() {
		for evt := range client.Events {
			switch evt.Type {
			case socketmode.EventTypeConnecting:
				log.Println("connecting to Slack...")
			case socketmode.EventTypeConnected:
				connected.Store(true)
				log.Println("connected to Slack")
			case socketmode.EventTypeDisconnect:
				connected.Store(false)
				log.Println("disconnected from Slack")
			case socketmode.EventTypeEventsAPI:
				client.Ack(*evt.Request)
				apiEvt, ok := evt.Data.(slackevents.EventsAPIEvent)
				if !ok {
					continue
				}
				if apiEvt.Type == slackevents.CallbackEvent {
					if msg, ok := apiEvt.InnerEvent.Data.(*slackevents.MessageEvent); ok {
						bot.HandleMessage(msg, api, db)
					}
				}
			case socketmode.EventTypeConnectionError:
				connected.Store(false)
				log.Printf("connection error: %v", evt.Data)
			case socketmode.EventTypeErrorBadMessage:
				log.Printf("bad message: %v", evt.Data)
			case socketmode.EventTypeIncomingError:
				log.Printf("incoming error: %v", evt.Data)
			}
		}
	}()

	log.Println("queue-bot ready")
	if err := client.RunContext(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("run: %v", err)
	}
	log.Println("shutting down")
}

// serveHealth exposes /healthz for k8s probes. It reports 200 only while the
// Slack socket is connected, so a dead connection is actually detected.
func serveHealth(addr string, connected *atomic.Bool) {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthHandler(connected))
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Printf("health server: %v", err)
	}
}

// healthHandler reports 200 only while the Slack socket is connected, so a dead
// connection is actually detected by the k8s probe.
func healthHandler(connected *atomic.Bool) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if connected.Load() {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("disconnected"))
	}
}

func mustEnv(key string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	log.Fatalf("required env var %s is not set", key)
	return ""
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
