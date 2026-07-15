// Command gateway starts the UDAL gRPC + REST gateway server.
package main

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	udalv1 "github.com/paulefl/udal/code/api/proto/gen/udal/v1"
	"github.com/paulefl/udal/code/gateway/internal/api"
	"github.com/paulefl/udal/code/gateway/internal/registry"
	"github.com/paulefl/udal/code/gateway/internal/service"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	grpcAddr := envOr("UDAL_GRPC_ADDR", ":50051")
	httpAddr := envOr("UDAL_HTTP_ADDR", ":8080")
	registryPath := envOr("UDAL_REGISTRY_PATH", "./udal-registry.db")
	tlsCertPath := os.Getenv("UDAL_TLS_CERT")
	tlsKeyPath := os.Getenv("UDAL_TLS_KEY")
	devInsecure := envOr("UDAL_DEV_INSECURE", "false") == "true"

	if tlsCertPath == "" && !devInsecure {
		log.Error("TLS is mandatory: set UDAL_TLS_CERT and UDAL_TLS_KEY, or explicitly opt out with UDAL_DEV_INSECURE=true for local development")
		os.Exit(1)
	}

	reg, err := registry.NewBboltRegistry(registryPath)
	if err != nil {
		log.Error("open device registry", "path", registryPath, "err", err)
		os.Exit(1)
	}
	defer reg.Close()

	props := api.NewMemoryPropertyStore()
	broker := api.NewBroker()
	svc := service.New(reg, props, broker)

	// ─── gRPC server ─────────────────────────────────────────────────────────
	var serverOpts []grpc.ServerOption
	var dialCreds credentials.TransportCredentials
	if tlsCertPath != "" {
		cert, err := tls.LoadX509KeyPair(tlsCertPath, tlsKeyPath)
		if err != nil {
			log.Error("load TLS certificate", "cert", tlsCertPath, "key", tlsKeyPath, "err", err)
			os.Exit(1)
		}
		serverOpts = append(serverOpts, grpc.Creds(credentials.NewTLS(&tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		})))
		// The REST gateway below dials this exact server on loopback, in the same
		// process — there is no network position for a MITM attacker to occupy, so
		// skipping hostname verification here is safe even though the cert's SAN
		// generally won't match the dial target.
		dialCreds = credentials.NewTLS(&tls.Config{InsecureSkipVerify: true}) //nolint:gosec // see comment above
	} else {
		log.Warn("starting without TLS (UDAL_DEV_INSECURE=true) — do not use in production")
		dialCreds = insecure.NewCredentials()
	}

	grpcServer := grpc.NewServer(serverOpts...)
	udalv1.RegisterDeviceServiceServer(grpcServer, svc)
	reflection.Register(grpcServer)

	lis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		log.Error("listen gRPC", "addr", grpcAddr, "err", err)
		os.Exit(1)
	}

	go func() {
		log.Info("gRPC server listening", "addr", grpcAddr, "tls", tlsCertPath != "")
		if err := grpcServer.Serve(lis); err != nil {
			log.Error("gRPC serve", "err", err)
		}
	}()

	// ─── grpc-gateway (REST) ──────────────────────────────────────────────────
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mux := runtime.NewServeMux()
	opts := []grpc.DialOption{grpc.WithTransportCredentials(dialCreds)}

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
		log.Info("REST gateway listening", "addr", httpAddr, "tls", tlsCertPath != "")
		var serveErr error
		if tlsCertPath != "" {
			serveErr = httpServer.ListenAndServeTLS(tlsCertPath, tlsKeyPath)
		} else {
			serveErr = httpServer.ListenAndServe()
		}
		if serveErr != nil && serveErr != http.ErrServerClosed {
			log.Error("HTTP serve", "err", serveErr)
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
