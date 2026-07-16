module github.com/paulefl/udal/code/gateway

go 1.24.0

require (
	github.com/golang-jwt/jwt/v5 v5.3.1
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.20.0
	github.com/paulefl/udal/code/api/proto/gen v0.0.0
	go.etcd.io/bbolt v1.4.3
	golang.org/x/crypto v0.48.0
	google.golang.org/grpc v1.79.3
	google.golang.org/protobuf v1.36.10
)

require (
	github.com/paulefl/udal/code/sdk/go v0.0.0
	golang.org/x/net v0.49.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
	golang.org/x/text v0.34.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20251202230838-ff82c1b0f217 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251202230838-ff82c1b0f217 // indirect
)

replace github.com/paulefl/udal/code/api/proto/gen => ../api/proto/gen

replace github.com/paulefl/udal/code/sdk/go => ../sdk/go
