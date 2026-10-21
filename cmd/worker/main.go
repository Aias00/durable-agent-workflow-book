// worker is an actual subprocess used by the crash-recovery experiments.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Aias00/durable-agent-workflow-book/lab"
)

func main() {
	schema := flag.String("schema", "", "isolated schema created by the experiment")
	endpoint := flag.String("tool", "", "loopback tool endpoint")
	name := flag.String("name", "worker-process", "worker owner")
	pause := flag.String("pause", "", "pause after_claim or after_tool until stdin newline (or SIGKILL)")
	gate := flag.Bool("start-gate", false, "wait for stdin newline before claiming")
	reconcile := flag.String("reconcile", "", "query this unknown workflow before running one ready item")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	input := bufio.NewReader(os.Stdin)
	waitGate := func() {
		ready := make(chan struct{})
		go func() { _, _ = input.ReadString('\n'); close(ready) }()
		select {
		case <-ready:
		case <-ctx.Done():
		}
	}
	store, err := lab.Open(ctx, *schema, false)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	defer store.Close()
	tool, err := lab.NewToolClient(*endpoint)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	defer tool.Close()
	worker := lab.Worker{Store: store, Tool: tool, Name: *name, Lease: 3 * time.Second, HeartbeatEvery: 500 * time.Millisecond, Backoff: 100 * time.Millisecond}
	worker.Hook = func(stage string, w lab.Workflow) {
		fmt.Printf("BOOK_STAGE %s %s %d\n", stage, w.ID, w.Fence)
		if *pause == stage {
			waitGate()
		}
	}
	if *gate {
		fmt.Println("BOOK_READY")
		waitGate()
	}
	if *reconcile != "" {
		err = worker.Reconcile(ctx, *reconcile)
	}
	if err == nil {
		err = worker.RunOne(ctx)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("BOOK_DONE")
}
