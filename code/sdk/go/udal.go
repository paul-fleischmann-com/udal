// Package udal is the Go client SDK for UDAL (Universal Device Abstraction
// Layer). It provides two entry points:
//
//   - [NewDevice] — the device side: register with a gateway, publish
//     property values, and handle incoming commands.
//   - [NewClient] — the application side: read/write device properties,
//     send commands, and subscribe to live property updates.
//
// Every operation returns a [*Error] on failure, wrapping the gRPC status
// code the gateway returned so callers can branch on it without importing
// grpc-go themselves.
package udal
