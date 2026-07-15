# Plan: #9 — Auth Middleware (API-Key + mTLS + OAuth2 + RBAC/ACL)

## Ausgangslage

Aktuell gibt es keinerlei Auth im Gateway — jeder mit Netzwerkzugriff auf den
(jetzt TLS-verschlüsselten, siehe #8) gRPC-Port kann jede RPC aufrufen. Dieses
Ticket bündelt vier Requirements aus req42.adoc §4.5: F-16 (API-Key), F-17 (mTLS),
F-18 (OAuth2 JWT Bearer), F-19 (RBAC), F-20 (per-Device ACL).

## Scope-Entscheidungen

- **Keine Management-API/CLI für Keys/ACLs in diesem Ticket.** Provisionierung
  läuft über einen einmaligen Bootstrap-Mechanismus (Env-Var). Verwaltung über
  API/CLI ist ein sinnvoller Folge-Issue (analog zur Aufteilung Capability
  Registry Service/#22 vs. CLI/#23).
- **"OAuth2 Device Flow" im Titel ≠ RFC 8628 Device Authorization Grant.** Die
  ACs testen ausschließlich JWT-Bearer-Validierung (Signatur via JWKS,
  `aud`/`iss`/`exp`). Wie der Client an das JWT kommt, ist nicht Sache des
  Gateways — das Ticket implementiert die Bearer-Token-Prüfung, nicht den
  vollständigen Device-Flow.
- **mTLS-Identität → Rolle `device`.** F-17 sagt nur "CN/SAN becomes the
  identity", legt aber keine Rollenzuordnung fest. Da mTLS-Clients in diesem
  Projekt typischerweise Geräte sind (SH-02), wird die CN als `DeviceID`
  interpretiert (Konvention: CN == Device-ID) und die Rolle fest auf `device`
  gesetzt. API-Key- und JWT-Identitäten tragen ihre Rolle explizit (durch den
  Operator vergeben bzw. als JWT-Claim `role`).
- **`DeleteDevice` fehlt in der F-19-Rollenmatrix.** Wird konservativ wie
  `RegisterDevice` behandelt (nur admin/operator), dokumentiert als bewusste
  Lücken-Auslegung.
- **ACL-Persistenz**: F-20 sagt explizit "in Device Registry alongside device
  entry" — `ACLEntry` wird ein neues Feld auf `api.Device`, automatisch über die
  bestehende JSON-Serialisierung in `BboltRegistry` mitpersistiert.
- **API-Key-Storage**: F-16 sagt nur "in Device Registry" (weniger eindeutig,
  da API-Keys nicht an ein einzelnes Device gebunden sind) — eigener Bucket in
  derselben bbolt-Datei, nicht im `Device`-Struct.

## Phasen

### Phase 1 — Identity/Role/Operation-Modell + RBAC/ACL-Entscheidungslogik
- Neues Package `code/gateway/internal/auth`: `Role`, `Identity`, `Operation`,
  RBAC-Matrix (aus F-19 Tabelle), `Authorize(id, op, deviceID, acl) error`
- `api.ACLEntry{Subject string; Allow bool}` + `ACL []ACLEntry` Feld auf `Device`
- Rein deterministische Logik, keine I/O — Unit-Tests für jede Matrix-Zelle plus
  ACL-Override-Fälle (allow overrides RBAC-deny, deny overrides RBAC-allow)

### Phase 2 — API-Key-Store
- `auth.APIKeyStore` Interface + bbolt-Implementierung (eigener Bucket in der
  bestehenden Registry-Datenbankdatei), bcrypt-Hashing (cost ≥ 12)
- Bootstrap via `UDAL_BOOTSTRAP_API_KEY=subject:role:rawkey` in `main.go`
  (idempotent: legt den Key nur an, falls `subject` noch nicht existiert)

### Phase 3 — mTLS-Identität
- gRPC-Server-TLS-Config um `ClientCAs` (aus `UDAL_MTLS_CA_CERT`) und
  `ClientAuth` erweitern: `RequireAndVerifyClientCert` wenn
  `UDAL_MTLS_REQUIRED=true`, sonst `VerifyClientCertIfGiven` (mTLS-optional,
  fällt durch zu API-Key/JWT)
- Identität aus `peer.FromContext` + `credentials.TLSInfo` extrahieren

### Phase 4 — OAuth2 JWT Bearer
- `github.com/golang-jwt/jwt/v5` + `github.com/MicahParks/keyfunc/v3` (JWKS-Fetch
  + Cache, vermeidet eigene JWK→Schlüssel-Krypto-Implementierung)
- Config: `UDAL_JWT_JWKS_URL`, `UDAL_JWT_AUDIENCE`, `UDAL_JWT_ISSUER`
- `role`-Claim optional, Default `reader` (least privilege) falls fehlend/ungültig

### Phase 5 — gRPC-Interceptor-Chain + Wiring
- Unary + Stream Interceptor: AuthN (mTLS → API-Key → JWT, erster Treffer
  gewinnt) dann AuthZ (RBAC + ACL via `auth.Authorize`)
- Device-ID-Extraktion aus der Request-Message per Type-Switch
- Wiring in `main.go` (`grpc.ChainUnaryInterceptor`/`ChainStreamInterceptor`)

### Phase 6 — Tests
- Unit-Tests pro Auth-Methode + Interceptor-Verhalten (Fehlercodes exakt wie
  in den ACs: `UNAUTHENTICATED`, `PERMISSION_DENIED`)
- Integrationstest: echter Client mit API-Key, echter Client mit mTLS-Zertifikat,
  RBAC-Deny + ACL-Override end-to-end

### Phase 7 — Doku + Changelog
