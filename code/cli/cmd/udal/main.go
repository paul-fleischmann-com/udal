// Command udal is the operator-facing CLI for the UDAL gateway (F-13,
// GitHub issue #23): publish, fetch, and list capability schemas against a
// running gateway's CapabilityService (#22).
package main

import (
	"fmt"
	"os"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		usage(os.Stderr)
		return 2
	}
	switch args[0] {
	case "schema":
		return runSchema(args[1:])
	case "-h", "--help", "help":
		usage(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "udal: unknown command %q\n", args[0])
		usage(os.Stderr)
		return 2
	}
}

func runSchema(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "udal: schema: expected a subcommand (publish, get, list)")
		return 2
	}
	switch args[0] {
	case "publish":
		return runSchemaPublishCmd(args[1:])
	case "get":
		return runSchemaGetCmd(args[1:])
	case "list":
		return runSchemaListCmd(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "udal: schema: unknown subcommand %q\n", args[0])
		return 2
	}
}

func usage(w *os.File) {
	_, _ = fmt.Fprint(w, `usage: udal <command> [arguments]

commands:
  schema publish <file.json>   publish a new capability schema version
  schema get <name>@<version>  fetch and print a published schema as JSON
  schema list [<name>]         list published schema versions, newest first

global flags (each subcommand accepts these):
  -gateway string   gateway gRPC address (default "localhost:50051", env UDAL_GATEWAY_ADDR)
  -api-key string   sent as the X-API-Key header (env UDAL_API_KEY)
  -ca string        path to a CA certificate to verify the gateway's server certificate (env UDAL_TLS_CA)
  -insecure         connect without TLS (env UDAL_DEV_INSECURE=true) — local development only
`)
}
