package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	udalv1 "github.com/paulefl/udal/code/api/proto/gen/udal/v1"
)

func runSchemaGetCmd(args []string) int {
	fs := flag.NewFlagSet("schema get", flag.ContinueOnError)
	cf := &connectFlags{}
	cf.register(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: udal schema get <name>@<version>")
		return 2
	}

	conn, err := cf.dial()
	if err != nil {
		fmt.Fprintf(os.Stderr, "udal: connect to gateway: %v\n", err)
		return 1
	}
	defer conn.Close()

	client := udalv1.NewCapabilityServiceClient(conn)
	return cmdSchemaGet(cf.authContext(context.Background()), client, os.Stdout, os.Stderr, fs.Arg(0))
}

// cmdSchemaGet fetches ref ("name@version") and prints the stored schema
// document as pretty-printed JSON (issue #23 AC2). See publish.go's
// cmdSchemaPublish for why this is split out from runSchemaGetCmd.
func cmdSchemaGet(ctx context.Context, client udalv1.CapabilityServiceClient, stdout, stderr io.Writer, ref string) int {
	name, version, ok := strings.Cut(ref, "@")
	if !ok || name == "" || version == "" {
		fmt.Fprintf(stderr, "udal: %q is not a valid name@version reference\n", ref)
		return 2
	}

	resp, err := client.GetSchema(ctx, &udalv1.GetSchemaRequest{Name: name, Version: version})
	if err != nil {
		fmt.Fprintf(stderr, "udal: %s\n", grpcMessage(err))
		return 1
	}

	raw := resp.GetSchema().GetRaw()
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, raw, "", "  "); err != nil {
		// Raw is always the exact document validated at publish time, so
		// this shouldn't happen — but a formatting hiccup shouldn't hide
		// the data itself.
		fmt.Fprintf(stderr, "udal: warning: could not pretty-print schema: %v\n", err)
		stdout.Write(raw)
		fmt.Fprintln(stdout)
		return 0
	}
	stdout.Write(pretty.Bytes())
	fmt.Fprintln(stdout)
	return 0
}
