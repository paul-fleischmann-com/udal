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
	canadapter "github.com/paulefl/udal/code/gateway/internal/adapters/can"
	httpadapter "github.com/paulefl/udal/code/gateway/internal/adapters/http"
	mqttadapter "github.com/paulefl/udal/code/gateway/internal/adapters/mqtt"
	"github.com/paulefl/udal/code/gateway/internal/api"
	"github.com/paulefl/udal/code/gateway/internal/auth"
	"github.com/paulefl/udal/code/gateway/internal/capability"
	"github.com/paulefl/udal/code/gateway/internal/config"
	"github.com/paulefl/udal/code/gateway/internal/heartbeat"
	"github.com/paulefl/udal/code/gateway/internal/logging"
	"github.com/paulefl/udal/code/gateway/internal/registry"
	"github.com/paulefl/udal/code/gateway/internal/service"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
)

func main() {
	// ─── Structured JSON logging (F-23, issue #28) ────────────────────────────
	// levelVar defaults to Info (its zero value) until UDAL_LOG_LEVEL is
	// parsed below — built before that parse, not after, so a bad
	// UDAL_LOG_LEVEL value can itself be logged (as JSON, at the default
	// level) rather than needing some earlier bootstrap-only logger.
	// baseLog carries no "component" attribute; every subsystem below
	// derives its own via .With("component", "…") (mqtt_adapter,
	// http_adapter, capability_registry, gateway.api, …) — log carries
	// "component"="gateway" for main.go's own top-level messages.
	levelVar := &slog.LevelVar{}
	baseLog := slog.New(logging.NewHandler(os.Stdout, levelVar))
	log := baseLog.With("component", "gateway")
	if lvl, err := logging.ParseLevel(os.Getenv("UDAL_LOG_LEVEL")); err != nil {
		log.Error("parse UDAL_LOG_LEVEL", "err", err)
		os.Exit(1)
	} else {
		levelVar.Set(lvl)
	}

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
	defer func() { _ = reg.Close() }()

	props := api.NewMemoryPropertyStore()
	broker := api.NewBroker()
	commands := api.NewCommandRouter()
	svc := service.New(reg, props, broker, commands)

	// ─── Capability Registry (F-13/F-14/F-15, issue #22) ──────────────────────
	// CapabilityService (Publish/Get/List) is always available, so schemas
	// can be published ahead of time — but enforcement (RegisterDevice/
	// SetProperty validating against a device's declared schema) is opt-in
	// via UDAL_CAPABILITY_ENFORCEMENT: turning it on unconditionally would
	// make every RegisterDevice call require a pre-published schema,
	// breaking any existing deployment whose devices don't have one yet.
	capabilityReg, err := capability.NewBboltRegistry(reg.DB(), capability.WithLogger(baseLog.With("component", "capability_registry")))
	if err != nil {
		log.Error("open capability registry", "err", err)
		os.Exit(1)
	}
	capSvc := service.NewCapabilityService(capabilityReg)
	if envOr("UDAL_CAPABILITY_ENFORCEMENT", "false") == "true" {
		svc.SetCapabilityRegistry(capabilityReg)
	}

	// ─── Heartbeat / online status (F-04, issue #42) ──────────────────────────
	presence := heartbeat.NewMonitor(reg, broker, time.Duration(cfg.Gateway.HeartbeatInterval), time.Duration(cfg.Gateway.DeviceTimeout))
	svc.SetPresenceMonitor(presence)
	presenceCtx, presenceCancel := context.WithCancel(context.Background())
	go presence.Run(presenceCtx)

	// ─── MQTT transport adapter (F-09) ────────────────────────────────────────
	var mqttAdapter *mqttadapter.Adapter
	if mqttBroker := config.ResolveString(os.Getenv("UDAL_MQTT_BROKER"), cfg.Gateway.Adapters.MQTT.Broker, ""); mqttBroker != "" {
		mqttAdapter = mqttadapter.New(mqttBroker, func(deviceID, path string, v api.PropertyValue) {
			broker.Publish(api.PropertyUpdate{DeviceID: deviceID, PropertyPath: path, Value: v, Timestamp: time.Now()})
		}, mqttadapter.WithLogger(baseLog.With("component", "mqtt_adapter")), mqttadapter.WithOnHeartbeat(func(deviceID string) {
			_ = presence.Touch(deviceID)
		}))
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

	// ─── HTTP transport adapter (F-10, issue #24) ─────────────────────────────
	// Unlike MQTT (gated behind a broker URL — there's no meaningful "off"
	// switch otherwise), the HTTP adapter is always constructed: it has no
	// global endpoint of its own, only per-device ones (Device.Labels, see
	// package httpadapter's doc comment), so devices with transport=http just
	// work without a separate opt-in.
	httpClient := &http.Client{}
	httpMTLSCert := config.ResolveString(os.Getenv("UDAL_HTTP_MTLS_CERT"), cfg.Gateway.Adapters.HTTP.MTLS.Cert, "")
	if httpMTLSCert != "" {
		httpMTLSKey := config.ResolveString(os.Getenv("UDAL_HTTP_MTLS_KEY"), cfg.Gateway.Adapters.HTTP.MTLS.Key, "")
		cert, err := tls.LoadX509KeyPair(httpMTLSCert, httpMTLSKey)
		if err != nil {
			log.Error("load HTTP adapter mTLS client certificate", "cert", httpMTLSCert, "key", httpMTLSKey, "err", err)
			os.Exit(1)
		}
		httpClient.Transport = &http.Transport{TLSClientConfig: &tls.Config{Certificates: []tls.Certificate{cert}}}
		log.Info("HTTP adapter presenting client certificate to devices", "cert", httpMTLSCert)
	}
	httpAdapterOpts := []httpadapter.Option{httpadapter.WithHTTPClient(httpClient), httpadapter.WithLogger(baseLog.With("component", "http_adapter"))}
	if pollInterval := time.Duration(cfg.Gateway.Adapters.HTTP.PollInterval); pollInterval > 0 {
		httpAdapterOpts = append(httpAdapterOpts, httpadapter.WithPollInterval(pollInterval))
	}
	httpAdapter := httpadapter.New(func(deviceID, path string, v api.PropertyValue) {
		broker.Publish(api.PropertyUpdate{DeviceID: deviceID, PropertyPath: path, Value: v, Timestamp: time.Now()})
	}, httpAdapterOpts...)
	svc.SetHTTPAdapter(httpAdapter)

	httpDevices, err := reg.List(registry.ListFilter{Transport: "http"})
	if err != nil {
		log.Error("list http devices", "err", err)
		os.Exit(1)
	}
	for _, d := range httpDevices {
		if err := httpAdapter.WatchDevice(context.Background(), d); err != nil {
			log.Warn("watch http device", "device", d.ID, "err", err)
		}
	}

	webhookAddr := config.ResolveAddr(os.Getenv("UDAL_HTTP_WEBHOOK_ADDR"), cfg.Gateway.Adapters.HTTP.WebhookPort, 8090)
	webhookServer := &http.Server{
		Addr:         webhookAddr,
		Handler:      httpAdapter.Handler(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	go func() {
		log.Info("HTTP adapter webhook receiver listening", "addr", webhookAddr)
		if err := webhookServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("HTTP webhook serve", "err", err)
		}
	}()

	// ─── CAN transport adapter (F-11, issue #25) ──────────────────────────────
	// Gated on UDAL_CAN_INTERFACE like MQTT's broker URL (there's no
	// meaningful "off" switch otherwise), unlike HTTP: a can0/vcan0 interface
	// either exists on the host or it doesn't, and req42.adoc TC-01 makes CAN
	// unavailable at all on non-Linux dev machines.
	var canAdapterInst *canadapter.Adapter
	if canIface := config.ResolveString(os.Getenv("UDAL_CAN_INTERFACE"), cfg.Gateway.Adapters.CAN.Interface, ""); canIface != "" {
		dbcPath := config.ResolveString(os.Getenv("UDAL_CAN_DBC_FILE"), cfg.Gateway.Adapters.CAN.DBCPath, "")
		if dbcPath == "" {
			log.Error("UDAL_CAN_DBC_FILE (or adapters.can.dbc_file) is required when the CAN adapter is enabled")
			os.Exit(1)
		}
		dbcFile, err := os.Open(dbcPath)
		if err != nil {
			log.Error("open DBC file", "path", dbcPath, "err", err)
			os.Exit(1)
		}
		canDB, err := canadapter.ParseDBC(dbcFile)
		_ = dbcFile.Close()
		if err != nil {
			log.Error("parse DBC file", "path", dbcPath, "err", err)
			os.Exit(1)
		}
		canAdapterInst = canadapter.New(canDB, func(deviceID, path string, v api.PropertyValue) {
			broker.Publish(api.PropertyUpdate{DeviceID: deviceID, PropertyPath: path, Value: v, Timestamp: time.Now()})
		}, canadapter.WithLogger(baseLog.With("component", "can_adapter")))
		if err := canAdapterInst.Open(canIface); err != nil {
			log.Error("open CAN interface", "interface", canIface, "err", err)
			os.Exit(1)
		}
		svc.SetCANAdapter(canAdapterInst)
		log.Info("CAN adapter listening", "interface", canIface, "dbc", dbcPath)

		canDevices, err := reg.List(registry.ListFilter{Transport: "can"})
		if err != nil {
			log.Error("list can devices", "err", err)
			os.Exit(1)
		}
		for _, d := range canDevices {
			if err := canAdapterInst.WatchDevice(context.Background(), d); err != nil {
				log.Warn("watch can device", "device", d.ID, "err", err)
			}
		}
	}

	// ─── Metrics/debug listener (issue #28) ───────────────────────────────────
	// Currently hosts only /debug/log-level (F-23 AC: "UDAL_LOG_LEVEL=debug
	// enables debug logs without restart" — see logging.LevelHandler's doc
	// comment for why this endpoint, not literally re-reading the env var,
	// is what makes that true). adapters.metrics_port/UDAL_METRICS_PORT has
	// been a parsed-but-unused config stub since issue #41; issue #27 will
	// add /health and /metrics to this same mux.
	metricsAddr := config.ResolveAddr(os.Getenv("UDAL_METRICS_ADDR"), cfg.Gateway.MetricsPort, 9090)
	metricsMux := http.NewServeMux()
	metricsMux.Handle("/debug/log-level", logging.LevelHandler(levelVar))
	metricsServer := &http.Server{
		Addr:         metricsAddr,
		Handler:      metricsMux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	go func() {
		log.Info("metrics/debug listener listening", "addr", metricsAddr)
		if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("metrics/debug serve", "err", err)
		}
	}()

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

	// requestLogger runs first in the interceptor chain (before auth) so a
	// request that fails authentication still gets a trace ID and a
	// logged outcome (F-23 AC: "Request log line includes trace_id").
	requestLogger := &logging.Interceptor{Log: baseLog.With("component", "gateway.api")}

	// ─── gRPC server ─────────────────────────────────────────────────────────
	serverOpts := []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(requestLogger.UnaryInterceptor, authenticator.UnaryInterceptor),
		grpc.ChainStreamInterceptor(requestLogger.StreamInterceptor, authenticator.StreamInterceptor),
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
	udalv1.RegisterCapabilityServiceServer(grpcServer, capSvc)
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
	if err := udalv1.RegisterCapabilityServiceHandlerFromEndpoint(ctx, mux, grpcAddr, opts); err != nil {
		log.Error("register capability REST gateway", "err", err)
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
	if canAdapterInst != nil {
		if err := canAdapterInst.Close(); err != nil {
			log.Error("CAN adapter close", "err", err)
		}
	}
	if err := webhookServer.Shutdown(shutCtx); err != nil {
		log.Error("HTTP webhook shutdown", "err", err)
	}
	if err := metricsServer.Shutdown(shutCtx); err != nil {
		log.Error("metrics/debug shutdown", "err", err)
	}
	httpAdapter.Close()
	presenceCancel()
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
