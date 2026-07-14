// Command gateway starts the UDAL gRPC + REST gateway server.
package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	udalv1 "github.com/paulefl/udal/api/gen/go/udal/v1"
	"github.com/paulefl/udal/gateway/internal/api"
	"github.com/paulefl/udal/gateway/internal/registry"
	"github.com/paulefl/udal/gateway/internal/service"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	grpcAddr := envOr("UDAL_GRPC_ADDR", ":50051")
	httpAddr := envOr("UDAL_HTTP_ADDR", ":8080")

	reg := registry.NewMemoryRegistry()
	props := api.NewMemoryPropertyStore()
	svc := service.New(reg, props)

	// ─── gRPC server ─────────────────────────────────────────────────────────
	grpcServer := grpc.NewServer()
	udalv1.RegisterDeviceServiceServer(grpcServer, svc)
	reflection.Register(grpcServer)

	lis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		log.Error("listen gRPC", "addr", grpcAddr, "err", err)
		os.Exit(1)
	}

	go func() {
		log.Info("gRPC server listening", "addr", grpcAddr)
		if err := grpcServer.Serve(lis); err != nil {
			log.Error("gRPC serve", "err", err)
		}
	}()

	// ─── grpc-gateway (REST) ──────────────────────────────────────────────────
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mux := runtime.NewServeMux()
	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}

	if err := udalv1.RegisterDeviceServiceHandlerFromEndpoint(ctx, mux, grpcAddr, opts); err != nil {
		log.Error("register REST gateway", "err", err)
		os.Exit(1)
	}

	httpServer := &http.Server{
		Addr:         httpAddr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		log.Info("REST gateway listening", "addr", httpAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("HTTP serve", "err", err)
		}
	}()

	// ─── Graceful shutdown ────────────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("shutting down")
	grpcServer.GracefulStop()
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutCancel()
	if err := httpServer.Shutdown(shutCtx); err != nil {
		log.Error("HTTP shutdown", "err", err)
	}
	log.Info("stopped")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
