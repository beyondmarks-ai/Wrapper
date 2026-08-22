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

	cloudtasks "cloud.google.com/go/cloudtasks/apiv2"
	"cloud.google.com/go/storage"
	"firebase.google.com/go/v4"

	"github.com/beyondmarks-ai/Wrapper/backend/control"
)

func main() {
	if err := run(); err != nil {
		slog.Error("Wrapper Cloud stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	projectID := requiredEnv("GOOGLE_CLOUD_PROJECT")
	bucketName := requiredEnv("WRAPPER_TRANSFER_BUCKET")
	location := envOr("WRAPPER_TASK_LOCATION", "us-central1")
	queue := envOr("WRAPPER_TASK_QUEUE", "wrapper-expiration")
	port := envOr("PORT", "8080")
	googleExchange, err := control.NewGoogleTokenExchanger(
		requiredEnv("WRAPPER_GOOGLE_CLIENT_ID"), requiredEnv("WRAPPER_GOOGLE_CLIENT_SECRET"), nil,
	)
	if err != nil {
		return err
	}

	firebaseApp, err := firebase.NewApp(ctx, &firebase.Config{ProjectID: projectID})
	if err != nil {
		return err
	}
	authClient, err := firebaseApp.Auth(ctx)
	if err != nil {
		return err
	}
	firestoreClient, err := firebaseApp.Firestore(ctx)
	if err != nil {
		return err
	}
	defer firestoreClient.Close()
	storageClient, err := storage.NewClient(ctx)
	if err != nil {
		return err
	}
	defer storageClient.Close()
	blobs, err := control.NewGCSBlobStore(ctx, storageClient, bucketName)
	if err != nil {
		return err
	}
	tasksClient, err := cloudtasks.NewClient(ctx)
	if err != nil {
		return err
	}
	defer tasksClient.Close()

	handler := control.NewServer(
		control.NewFirestoreStore(firestoreClient), blobs,
		control.NewCloudTaskScheduler(tasksClient, projectID, location, queue),
		control.NewFirebaseVerifier(authClient), control.NewFirestoreInvites(firestoreClient), googleExchange,
	)
	server := &http.Server{
		Addr: ":" + port, Handler: handler, ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 15 * time.Second, WriteTimeout: 40 * time.Second, IdleTimeout: 90 * time.Second,
		MaxHeaderBytes: 32 << 10,
	}
	serverErrors := make(chan error, 1)
	go func() {
		slog.Info("Wrapper Cloud listening", "port", port)
		serverErrors <- server.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err = <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func requiredEnv(name string) string {
	value := os.Getenv(name)
	if value == "" {
		slog.Error("Required environment variable is missing", "name", name)
		os.Exit(2)
	}
	return value
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
