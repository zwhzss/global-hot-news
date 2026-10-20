package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ai-informations/internal/bindings"
	"ai-informations/internal/config"
	"ai-informations/internal/logger"
	"ai-informations/internal/repository"
	"ai-informations/internal/router"
	"ai-informations/internal/service/classifier"
	"ai-informations/internal/service/collector"
	"ai-informations/internal/service/datasource"
	"ai-informations/internal/service/filter"
	"ai-informations/internal/service/follow"
	"ai-informations/internal/service/hotrank"
	"ai-informations/internal/service/llmqueue"
	"ai-informations/internal/service/news"
	"ai-informations/internal/service/parser"
	"ai-informations/internal/service/recommend"
	"ai-informations/internal/service/robots"
	"ai-informations/internal/service/setting"
	"ai-informations/internal/service/translator"
	"ai-informations/internal/service/tts"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

//go:embed all:frontend_dist
var assets embed.FS

func main() {
	// Initialize app data directory
	appDataDir := filepath.Join(os.Getenv("APPDATA"), "GlobalHotNews")
	if err := os.MkdirAll(appDataDir, 0755); err != nil {
		log.Fatalf("Failed to create app data directory: %v", err)
	}

	dbPath := filepath.Join(appDataDir, "global_hotnews.db")
	logger.Init(filepath.Join(appDataDir, "app.log"))

	// Narration audio for fetched articles: <appDataDir>/media/tts/<id>.mp3,
	// served to the WebView through the /media/ branch of assetHandler.
	mediaDir := filepath.Join(appDataDir, "media")
	if err := os.MkdirAll(filepath.Join(mediaDir, "tts"), 0755); err != nil {
		log.Printf("Warning: failed to create media directory: %v", err)
	}

	// WAL keeps readers unblocked while a job writes, and the busy timeout
	// absorbs the bursts of small writes a parallel LLM job produces
	// (llm_max_concurrency > 1); without them SQLite can report "database is
	// locked" under concurrent article updates.
	db, err := gorm.Open(sqlite.Open(dbPath+"?_busy_timeout=10000&_journal_mode=WAL"), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}

	if err := repository.AutoMigrate(db); err != nil {
		log.Fatalf("Failed to migrate database: %v", err)
	}

	// Seed default data
	if err := repository.SeedDefaultCategories(db); err != nil {
		log.Printf("Warning: failed to seed categories: %v", err)
	}
	if err := repository.SeedDefaultSettings(db); err != nil {
		log.Printf("Warning: failed to seed settings: %v", err)
	}
	// Remove sources left behind with no name/type/url before seeding. These were
	// produced by a full-row Save on a partial struct during fetch; clearing them
	// first lets SeedDefaultSources re-create the real entries below.
	if n, err := repository.NewSourceRepository(db).DeleteBlankSources(); err != nil {
		log.Printf("Warning: failed to clean blank sources: %v", err)
	} else if n > 0 {
		log.Printf("Removed %d blank data source rows", n)
	}
	if err := repository.SeedDefaultSources(db); err != nil {
		log.Printf("Warning: failed to seed sources: %v", err)
	}

	// Initialize config
	cfg, err := config.Load(appDataDir)
	if err != nil {
		log.Printf("Warning: failed to load config, using defaults: %v", err)
		cfg = config.DefaultConfig()
	}

	// Initialize services
	robotParser := robots.NewParser()
	parserFactory := parser.NewFactory()
	filterService := filter.NewService(db)
	classifyService := classifier.NewService(db)
	dsService := datasource.NewService(db, robotParser, parserFactory, filterService, classifyService, cfg)
	collectorService := collector.NewService(db, dsService, filterService, cfg)
	hotrankService := hotrank.NewService(db)
	followService := follow.NewService(db)
	recommendService := recommend.NewService(db)
	newsService := news.NewService(db)
	settingService := setting.NewService(db)
	translatorService := translator.NewService(db)
	ttsService := tts.NewService(db, mediaDir)

	// Wire the LLM fallback into the classifier: when keyword matching cannot
	// place an article, the model gets a chance before the article is left
	// uncategorized. A getter (not a fixed provider) so saving cls_* settings
	// on the settings page applies immediately, without a restart.
	classifyService.SetLLMProviderGetter(func() classifier.LLMProvider {
		p, _ := translatorService.ClassifierProvider()
		if p == nil {
			return nil // avoid returning a typed nil as the interface
		}
		return p
	})

	// Initialize RSSHub instance from settings (empty = use default rsshub.app)
	if inst, err := settingService.Get("rsshub_instance"); err == nil && strings.TrimSpace(inst) != "" {
		parser.SetRSSHubInstance(inst)
	}

	// The LLM queue serialises every task that talks to the model: one job at
	// a time, FIFO, cancellable from the settings page. Nothing below starts a
	// model call automatically — the local endpoint stays free until the user
	// presses a button.
	queue := llmqueue.New()

	// Infrastructure-only startup. The Wails binding generator (built with
	// -tags bindings) calls wails.Run and returns immediately without
	// invoking OnStartup, so anything slow here would stall "Generating
	// bindings" during `wails build` — keep it in OnStartup, not in main().
	var apiServer *http.Server
	startInfra := func() {
		// Upgrade articles written before the bilingual columns existed
		// (language detection + column copy, no model involved).
		if n, err := repository.BackfillBilingual(db); err != nil {
			log.Printf("Warning: failed to backfill bilingual fields: %v", err)
		} else if n > 0 {
			log.Printf("Backfilled bilingual fields for %d articles", n)
		}

		// Keep the hot-rank snapshot usable without a manual "Recalculate",
		// and hand this round's new articles to TTS, which synthesises on its
		// own goroutine (需求: 拉取结束启动协程进行语音合成).
		collectorService.SetCycleHook(func(newArticleIDs []uint) {
			if err := hotrankService.EnsureFresh(30 * time.Minute); err != nil {
				log.Printf("Warning: hot rank refresh failed: %v", err)
			}
			if len(newArticleIDs) > 0 {
				ttsService.RoundComplete(newArticleIDs)
			}
		})

		// Start collector scheduler (respect auto_collect setting)
		autoCollect := "true"
		if v, err := settingService.Get("auto_collect"); err == nil {
			autoCollect = v
		}
		if autoCollect == "true" {
			collectorService.Start()
		}

		// Start the local HTTP API server so external tools can query the app.
		_, apiServer = router.SetupLocalAPI(cfg.APIPort, cfg.APIToken)
		go func() {
			addr := fmt.Sprintf(":%d", cfg.APIPort)
			log.Printf("Local API server listening on http://127.0.0.1%s", addr)
			if err := apiServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Printf("Warning: local API server failed: %v", err)
			}
		}()
	}

	// Create Wails bindings
	app := bindings.NewBindings(dsService, collectorService, hotrankService, followService, recommendService, newsService, settingService, translatorService, classifyService, ttsService, queue)

	err = wails.Run(&options.App{
		Title:     "Global Hot News",
		Width:     1280,
		Height:    800,
		MinWidth:  1024,
		MinHeight: 600,
		AssetServer: &assetserver.Options{
			Handler: assetHandler(mediaDir),
		},
		OnStartup: func(ctx context.Context) {
			logger.Info("Application started")
			// Infrastructure only; heavy work runs in the background so the
			// UI stays responsive. No model call starts automatically.
			go startInfra()
		},
		OnShutdown: func(ctx context.Context) {
			// Stop model work first so no queued job outlives the window.
			queue.Stop()
			collectorService.Stop()
			translatorService.Stop()
			ttsService.Stop()
			if apiServer != nil {
				_ = apiServer.Shutdown(ctx)
			}
			logger.Info("Application shutdown")
		},
		OnBeforeClose: func(ctx context.Context) (prevent bool) {
			// Signal collector to stop (non-blocking) and allow window to close immediately.
			collectorService.Stop()
			return false
		},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			DisableWindowIcon:    false,
			Theme:                windows.SystemDefault,
		},
		Bind: []interface{}{app},
	})

	if err != nil {
		log.Fatalf("Failed to start application: %v", err)
	}
}

// assetHandler serves embedded frontend assets and handles SPA routing.
// mediaRoot (if non-empty) additionally exposes /media/* as plain files — the
// narration mp3s the WebView's <audio> elements stream; http.Dir blocks any
// attempt to walk above mediaRoot.
func assetHandler(mediaRoot string) http.Handler {
	var mediaFS http.Handler
	if mediaRoot != "" {
		mediaFS = http.StripPrefix("/media/", http.FileServer(http.Dir(mediaRoot)))
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if mediaFS != nil && strings.HasPrefix(r.URL.Path, "/media/") {
			// audio/mpeg for .mp3 comes from the platform MIME table; default
			// it here for systems where it is missing.
			if mime.TypeByExtension(filepath.Ext(r.URL.Path)) == "" {
				w.Header().Set("Content-Type", "application/octet-stream")
			}
			mediaFS.ServeHTTP(w, r)
			return
		}

		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		data, err := fs.ReadFile(assets, "frontend_dist/"+path)
		if err != nil {
			// SPA fallback: return index.html for unknown routes
			data, err = fs.ReadFile(assets, "frontend_dist/index.html")
			if err != nil {
				http.Error(w, "Not found", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(data)
			return
		}

		// Set Content-Type based on file extension
		ct := mime.TypeByExtension(filepath.Ext(path))
		if ct == "" {
			ct = "application/octet-stream"
		}
		if strings.Contains(ct, "text/") || strings.Contains(ct, "javascript") || strings.Contains(ct, "json") {
			ct += "; charset=utf-8"
		}
		w.Header().Set("Content-Type", ct)
		w.Write(data)
	})
}
