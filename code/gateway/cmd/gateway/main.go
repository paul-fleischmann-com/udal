// Command gateway starts the UDAL gRPC + REST gateway server.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	udalv1 "github.com/paulefl/udal/code/api/proto/gen/udal/v1"
	mqttadapter "github.com/paulefl/udal/code/gateway/internal/adapters/mqtt"
	"github.com/paulefl/udal/code/gateway/internal/api"
	"github.com/paulefl/udal/code/gateway/internal/auth"
	"github.com/paulefl/udal/code/gateway/internal/config"
	"github.com/paulefl/udal/code/gateway/internal/registry"
	"github.com/paulefl/udal/code/gateway/internal/service"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// ─── Config file (F-09/req42.adoc §7.2, issue #41) ────────────────────────
	// A missing gateway.yaml is not an error — cfg stays zero-value, and every
	// setting below resolves to exactly its pre-#41 default via
	// config.ResolveString/ResolveAddr, so existing deployments with no config
	// file and only the flat UDAL_* env vars are unaffected.
	configFlag := flag.String("config", "", "path to gateway.yaml (default: $UDAL_CONFIG_PATH or ./gateway.yaml)")
	flag.Parse()
	configPath := *configFlag
	if configPath == "" {
		configPath = envOr("UDAL_CONFIG_PATH", "gateway.yaml")
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		log.Error("load config file", "path", configPath, "err", err)
		os.Exit(1)
	}
	if err := cfg.ApplyEnv(); err != nil {
		log.Error("apply config env overrides", "err", err)
		os.Exit(1)
	}

	grpcAddr := config.ResolveAddr(os.Getenv("UDAL_GRPC_ADDR"), cfg.Gateway.GRPCPort, 50051)
	httpAddr := config.ResolveAddr(os.Getenv("UDAL_HTTP_ADDR"), cfg.Gateway.HTTPPort, 8080)
	registryPath := config.ResolveString(os.Getenv("UDAL_REGISTRY_PATH"), cfg.Gateway.Registry.Path, "./udal-registry.db")
	tlsCertPath := config.ResolveString(os.Getenv("UDAL_TLS_CERT"), cfg.Gateway.TLS.Cert, "")
	tlsKeyPath := config.ResolveString(os.Getenv("UDAL_TLS_KEY"), cfg.Gateway.TLS.Key, "")
	devInsecure := envOr("UDAL_DEV_INSECURE", "false") == "true"
	mtlsCACertPath := config.ResolveString(os.Getenv("UDAL_MTLS_CA_CERT"), cfg.Gateway.TLS.CA, "")
	mtlsRequired := envOr("UDAL_MTLS_REQUIRED", "false") == "true"
	bootstrapAPIKey := os.Getenv("UDAL_BOOTSTRAP_API_KEY")
	jwksURL := config.ResolveString(os.Getenv("UDAL_JWT_JWKS_URL"), cfg.Gateway.Auth.JWKSURL, "")
	jwtAudience := os.Getenv("UDAL_JWT_AUDIENCE")
	jwtIssuer := os.Getenv("UDAL_JWT_ISSUER")

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
	commands := api.NewCommandRouter()
	svc := service.New(reg, props, broker, commands)

	// ─── MQTT transport adapter (F-09) ────────────────────────────────────────
	var mqttAdapter *mqttadapter.Adapter
	if mqttBroker := config.ResolveString(os.Getenv("UDAL_MQTT_BROKER"), cfg.Gateway.Adapters.MQTT.Broker, ""); mqttBroker != "" {
		mqttAdapter = mqttadapter.New(mqttBroker, func(deviceID, path string, v api.PropertyValue) {
			broker.Publish(api.PropertyUpdate{DeviceID: deviceID, PropertyPath: path, Value: v, Timestamp: time.Now()})
		}, mqttadapter.WithLogger(log))
		if err := mqttAdapter.Connect(context.Background()); err != nil {
			log.Error("connect to MQTT broker", "broker", mqttBroker, "err", err)
			os.Exit(1)
		}
		svc.SetMQTTAdapter(mqttAdapter)
		log.Info("connected to MQTT broker", "broker", mqttBroker)

		mqttDevices, err := reg.List(registry.ListFilter{Transport: "mqtt"})
		if err != nil {
			log.Error("list mqtt devices", "err", err)
			os.Exit(1)
		}
		for _, d := range mqttDevices {
			if err := mqttAdapter.WatchDevice(context.Background(), d.ID); err != nil {
				log.Warn("watch mqtt device", "device", d.ID, "err", err)
			}
		}
	}

	// ─── Auth (F-16/F-17/F-18/F-19/F-20) ──────────────────────────────────────
	apiKeys, err := auth.NewAPIKeyStore(reg.DB())
	if err != nil {
		log.Error("open API key store", "err", err)
		os.Exit(1)
	}
	if bootstrapAPIKey != "" {
		parts := strings.SplitN(bootstrapAPIKey, ":", 3)
		if len(parts) != 3 {
			log.Error("UDAL_BOOTSTRAP_API_KEY must be 'subject:role:rawkey'")
			os.Exit(1)
		}
		subject, role, rawKey := parts[0], auth.Role(parts[1]), parts[2]
		has, err := apiKeys.Has(subject)
		if err != nil {
			log.Error("check bootstrap API key", "err", err)
			os.Exit(1)
		}
		if !has {
			if err := apiKeys.Put(subject, role, rawKey); err != nil {
				log.Error("provision bootstrap API key", "err", err)
				os.Exit(1)
			}
			log.Info("provisioned bootstrap API key", "subject", subject, "role", role)
		}
	}

	var jwtValidator *auth.JWTValidator
	if jwksURL != "" {
		jwtValidator, err = auth.NewJWTValidator(jwksURL, jwtAudience, jwtIssuer)
		if err != nil {
			log.Error("initialize JWT validator", "jwks_url", jwksURL, "err", err)
			os.Exit(1)
		}
	}
	authenticator := &auth.Authenticator{APIKeys: apiKeys, JWT: jwtValidator}

	// ─── gRPC server ─────────────────────────────────────────────────────────
	serverOpts := []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(authenticator.UnaryInterceptor),
		grpc.ChainStreamInterceptor(authenticator.StreamInterceptor),
	}
	var dialCreds credentials.TransportCredentials
	// tlsConfig is shared with the HTTP listener below (Server.TLSConfig) so
	// mTLS requirements apply to both — Server.ListenAndServeTLS on its own
	// builds a minimal config from the cert/key files and ignores ClientAuth.
	var tlsConfig *tls.Config
	if tlsCertPath != "" {
		cert, err := tls.LoadX509KeyPair(tlsCertPath, tlsKeyPath)
		if err != nil {
			log.Error("load TLS certificate", "cert", tlsCertPath, "key", tlsKeyPath, "err", err)
			os.Exit(1)
		}
		tlsConfig = &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		}
		if mtlsCACertPath != "" {
			caPEM, err := os.ReadFile(mtlsCACertPath)
			if err != nil {
				log.Error("read mTLS CA certificate", "path", mtlsCACertPath, "err", err)
				os.Exit(1)
			}
			caPool := x509.NewCertPool()
			if !caPool.AppendCertsFromPEM(caPEM) {
				log.Error("parse mTLS CA certificate: no valid certificates found", "path", mtlsCACertPath)
				os.Exit(1)
			}
			tlsConfig.ClientCAs = caPool
			if mtlsRequired {
				tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
				// Exempt loopback connections — i.e. the REST gateway's own
				// internal dial below — from the client-cert requirement.
				// Without this, that internal hop would need to present some
				// certificate purely to satisfy the handshake, and whatever
				// cert it presented would resolve to a RoleDevice identity
				// (see auth.IdentityFromContext), silently overriding
				// whatever API-Key/JWT credential the original external
				// caller actually presented on every single REST request.
				//
				// A fresh *tls.Config is built (rather than copying
				// *tlsConfig by value) because tls.Config embeds a mutex.
				loopbackExempt := &tls.Config{
					Certificates: tlsConfig.Certificates,
					ClientCAs:    tlsConfig.ClientCAs,
					MinVersion:   tlsConfig.MinVersion,
					ClientAuth:   tls.NoClientCert,
				}
				tlsConfig.GetConfigForClient = func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
					if isLoopback(hello.Conn.RemoteAddr()) {
						return loopbackExempt, nil
					}
					return nil, nil // nil: caller falls back to the original config
				}
			} else {
				// F-17 "mTLS-optional mode": requests without a client cert
				// fall through to API-Key/JWT auth instead of failing the
				// handshake outright.
				tlsConfig.ClientAuth = tls.VerifyClientCertIfGiven
			}
		}
		serverOpts = append(serverOpts, grpc.Creds(credentials.NewTLS(tlsConfig)))
		// The REST gateway below dials this exact server on loopback, in the same
		// process — there is no network position for a MITM attacker to occupy, so
		// skipping hostname verification here is safe even though the cert's SAN
		// generally won't match the dial target. It never needs to present a
		// client certificate itself: either mTLS isn't required, mTLS is
		// optional (no cert is still accepted), or mTLS is required and this
		// loopback connection is exempted above.
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

	// grpc-gateway only forwards a curated set of incoming HTTP headers to
	// gRPC metadata by default (DefaultHeaderMatcher) — neither X-API-Key
	// (F-16) nor Authorization (F-18) is in it, so REST clients using either
	// would be silently rejected as unauthenticated without this.
	mux := runtime.NewServeMux(runtime.WithIncomingHeaderMatcher(func(key string) (string, bool) {
		switch strings.ToLower(key) {
		case "x-api-key", "authorization":
			return key, true
		default:
			return runtime.DefaultHeaderMatcher(key)
		}
	}))
	opts := []grpc.DialOption{grpc.WithTransportCredentials(dialCreds)}

	if err := udalv1.RegisterDeviceServiceHandlerFromEndpoint(ctx, mux, grpcAddr, opts); err != nil {
		log.Error("register REST gateway", "err", err)
		os.Exit(1)
	}

	httpServer := &http.Server{
		Addr:         httpAddr,
		Handler:      mux,
		TLSConfig:    tlsConfig,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		log.Info("REST gateway listening", "addr", httpAddr, "tls", tlsCertPath != "")
		var serveErr error
		if tlsCertPath != "" {
			// Cert/key already loaded into httpServer.TLSConfig.Certificates above.
			serveErr = httpServer.ListenAndServeTLS("", "")
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
	if mqttAdapter != nil {
		if err := mqttAdapter.Disconnect(shutCtx); err != nil {
			log.Error("MQTT disconnect", "err", err)
		}
	}
	log.Info("stopped")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// isLoopback reports whether addr's host is a loopback address (127.0.0.0/8
// or ::1) — used to exempt the REST gateway's own internal dial from a
// required client certificate.
func isLoopback(addr net.Addr) bool {
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
