// Command roomd runs the EVDR Room Service (src/services/room): an HTTP JSON
// service exposing room/document/grant operations over the frozen Room SPI
// (TR-10.1).
//
// Environment:
//
//	EVDR_ROOM_ADDR                  listen address (default ":8080")
//	EVDR_ROOM_ADAPTER               "mem" (default, dev) or "nextcloud"
//	EVDR_ROOM_SERVICE_DEV_SECRET    shared dev secret (required; do not log)
//	EVDR_ROOM_SERVICE_DEV_TENANT    tenant for the dev principal (default "dev")
//	EVDR_ROOM_MAX_UPLOAD_BYTES      upload ceiling (default 104857600 = 100 MiB)
//
// Nextcloud adapter (EVDR_ROOM_ADAPTER=nextcloud) additionally requires:
//
//	EVDR_NEXTCLOUD_BASE_URL
//	EVDR_NEXTCLOUD_USERNAME
//	EVDR_NEXTCLOUD_APP_PASSWORD
//
// The adapter binds one tenant at construction (the SPI's tenant-bound
// posture); the dev principal's tenant must equal the adapter's tenant or
// every SPI call fails with the SPI's own sentinels.
//
// Production replaces the dev secret with OIDC/JWT validation (TR-6.x) and
// NATS JetStream event wiring (TR-7.1) behind the seams in this package —
// no code paths here change shape.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/ivenkwan/evdr/src/services/room"
	"github.com/ivenkwan/evdr/src/spi"
	"github.com/ivenkwan/evdr/src/spi/adapters/nextcloud"
	"github.com/ivenkwan/evdr/src/spi/conformance/memadapter"
)

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	if err := run(logger); err != nil {
		logger.Error("roomd exited with error", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	addr := getenv("EVDR_ROOM_ADDR", ":8080")

	tenantID := spi.TenantID(getenv("EVDR_ROOM_SERVICE_DEV_TENANT", "dev"))
	secret := os.Getenv("EVDR_ROOM_SERVICE_DEV_SECRET")
	if secret == "" {
		return errors.New("EVDR_ROOM_SERVICE_DEV_SECRET is required (never default a secret); refusing to start")
	}

	// Adapter selection. Default is the in-memory reference adapter so the
	// slice runs with zero dependencies; production tiers select "nextcloud"
	// (Tier 0/2/3, one instance per tenant) or, from Phase 2.5, the native
	// adapter — both behind the same spi.RoomSPI constructor seam.
	adapterKind := getenv("EVDR_ROOM_ADAPTER", "mem")
	var backend spi.RoomSPI
	switch adapterKind {
	case "mem":
		backend = memadapter.New(tenantID)
	case "nextcloud":
		nc, err := nextcloud.New(nextcloud.Config{
			BaseURL:     os.Getenv("EVDR_NEXTCLOUD_BASE_URL"),
			Username:    os.Getenv("EVDR_NEXTCLOUD_USERNAME"),
			AppPassword: os.Getenv("EVDR_NEXTCLOUD_APP_PASSWORD"),
			TenantID:    tenantID,
			// Renderer wiring (Office→PDF, watermarking) is owned by the
			// viewer pipeline (TR-4.x) — GetRenderStream stays ErrUnsupported
			// until that service wires in.
		})
		if err != nil {
			return err
		}
		backend = nc
	default:
		return errors.New("EVDR_ROOM_ADAPTER must be one of: mem, nextcloud")
	}

	maxUpload := int64(100 << 20) // 100 MiB
	if v := os.Getenv("EVDR_ROOM_MAX_UPLOAD_BYTES"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n <= 0 {
			return errors.New("EVDR_ROOM_MAX_UPLOAD_BYTES must be a positive integer")
		}
		maxUpload = n
	}

	auth, err := room.NewDevAuthenticator(room.DevPrincipalConfig{
		Secret:      secret,
		TenantID:    tenantID,
		UserID:      getenv("EVDR_ROOM_SERVICE_DEV_USER_ID", "admin@evdr.local"),
		DisplayName: getenv("EVDR_ROOM_SERVICE_DEV_USER_NAME", "EVDR Admin"),
		Kind:        spi.ActorInternalUser,
	})
	if err != nil {
		return err
	}
	srv, err := room.New(room.Config{
		SPI:            backend,
		Auth:           auth,
		Events:         room.NewSlogEventSink(logger),
		Logger:         logger,
		MaxUploadBytes: maxUpload,
	})
	if err != nil {
		return err
	}

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       2 * time.Minute,  // multipart uploads may take a while
		WriteTimeout:      10 * time.Minute, // export streaming
		IdleTimeout:       2 * time.Minute,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("room service listening", "addr", addr, "adapter", adapterKind, "tenant", tenantID, "spi_contract", spi.ContractVersion)
		errCh <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		logger.Info("shutdown signal received")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	}
}
