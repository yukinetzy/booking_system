package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"easybook/internal/config"
	"easybook/internal/db"
	"easybook/internal/handlers"
	"easybook/internal/middleware"
	"easybook/internal/models"
	"easybook/internal/session"
	"easybook/internal/view"
)

func main() {
	env, err := config.Load()
	if err != nil {
		log.Fatalf("Startup failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	mongoClient, database, err := db.Connect(ctx, env)
	if err != nil {
		log.Fatalf("Startup failed: %v", err)
	}
	defer func() {
		_ = mongoClient.Disconnect(context.Background())
	}()

	maintenanceCtx, maintenanceCancel := context.WithTimeout(context.Background(), 30*time.Second)
	if err := db.EnsureStartupMaintenance(maintenanceCtx, database); err != nil {
		maintenanceCancel()
		log.Fatalf("Startup failed: %v", err)
	}
	maintenanceCancel()

	sessionCtx, sessionCancel := context.WithTimeout(context.Background(), 10*time.Second)
	sessionManager, err := session.NewManager(sessionCtx, database, env.IsProduction, env.SessionSecret)
	sessionCancel()
	if err != nil {
		log.Fatalf("Startup failed: %v", err)
	}

	store := models.NewStore(database)
	renderer := view.NewRenderer("views")
	app := handlers.NewApp(env, store, sessionManager, renderer, "views")

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", env.Port),
		Handler:      app.Router(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		middleware.LogStartup(env.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	signalChannel := make(chan os.Signal, 1)
	signal.Notify(signalChannel, syscall.SIGINT, syscall.SIGTERM)
	<-signalChannel

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = server.Shutdown(shutdownCtx)
}
