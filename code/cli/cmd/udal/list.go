package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	udalv1 "github.com/paulefl/udal/code/api/proto/gen/udal/v1"
)

func runSchemaListCmd(args []string) int {
	fs := flag.NewFlagSet("schema list", flag.ContinueOnError)
	cf := &connectFlags{}
	cf.register(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 1 {
		fmt.Fprintln(os.Stderr, "usage: udal schema list [<name>]")
		return 2
	}
	var name string
	if fs.NArg() == 1 {
		name = fs.Arg(0)
	}

	conn, err := cf.dial()
	if err != nil {
		fmt.Fprintf(os.Stderr, "udal: connect to gateway: %v\n", err)
		return 1
	}
	defer conn.Close()

	client := udalv1.NewCapabilityServiceClient(conn)
	return cmdSchemaList(cf.authContext(context.Background()), client, os.Stdout, os.Stderr, name)
}

// cmdSchemaList lists schemas (optionally filtered by name), newest first
// (issue #23 AC3). CapabilityService/capability.Registry (#22) don't
// guarantee any particular order, so the sort happens here — see the plan
// doc's Design-Entscheidungen for why that's a CLI-side decision rather
// than a change to the already-merged service. See publish.go's
// cmdSchemaPublish for why this is split out from runSchemaListCmd.
func cmdSchemaList(ctx context.Context, client udalv1.CapabilityServiceClient, stdout, stderr io.Writer, name string) int {
	resp, err := client.ListSchemas(ctx, &udalv1.ListSchemasRequest{Name: name})
	if err != nil {
		fmt.Fprintf(stderr, "udal: %s\n", grpcMessage(err))
		return 1
	}

	schemas := resp.GetSchemas()
	sort.Slice(schemas, func(i, j int) bool {
		return schemas[i].GetPublishedAt().AsTime().After(schemas[j].GetPublishedAt().AsTime())
	})

	if len(schemas) == 0 {
		fmt.Fprintln(stdout, "no schemas published")
		return 0
	}
	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tVERSION\tPUBLISHED")
	for _, s := range schemas {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", s.GetName(), s.GetVersion(), s.GetPublishedAt().AsTime().Format(time.RFC3339))
	}
	if err := tw.Flush(); err != nil {
		fmt.Fprintf(stderr, "udal: write output: %v\n", err)
		return 1
	}
	return 0
}
