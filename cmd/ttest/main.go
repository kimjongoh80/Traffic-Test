// Command ttest is a traffic stress-test tool for network equipment.
//
//	ttest                (no arguments: start the web GUI)
//	ttest gui [-web 127.0.0.1:8080]
//	ttest server [-p 5210]
//	ttest client -c <server> [-u] [-R] [-t 10] [-P 4] [-b 1G] [-l 1460] [--json out.json]
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"

	"github.com/kimjongoh80/Traffic-Test/internal/engine"
	"github.com/kimjongoh80/Traffic-Test/internal/gui"
	"github.com/kimjongoh80/Traffic-Test/internal/report"
)

// version is overridden at build time with -ldflags "-X main.version=..."
var version = "0.6.2"

func usage() {
	fmt.Fprintf(os.Stderr, `Traffic-Test %s - network traffic stress test

Usage:
  ttest                         start the web GUI (same as "ttest gui")
  ttest gui [options]           app window (Server/Client mode)
  ttest server [options]        run on the PC at one end of the device under test
  ttest client -c HOST [opts]   run on the PC at the other end

Run "ttest gui -h", "ttest server -h" or "ttest client -h" for options.
`, version)
}

func main() {
	if len(os.Args) < 2 {
		runGUI(nil)
		return
	}
	switch os.Args[1] {
	case "gui":
		runGUI(os.Args[2:])
	case "server", "-s":
		runServer(os.Args[2:])
	case "client", "-c":
		args := os.Args[2:]
		if os.Args[1] == "-c" {
			args = append([]string{"-c"}, args...)
		}
		runClient(args)
	case "version", "-v", "--version":
		fmt.Println("ttest", version)
	default:
		usage()
		os.Exit(2)
	}
}

func runGUI(args []string) {
	fs := flag.NewFlagSet("gui", flag.ExitOnError)
	web := fs.String("web", "127.0.0.1:8080", "address of the web GUI (use 0.0.0.0:8080 to allow remote browsers)")
	port := fs.Int("p", engine.DefaultPort, "test server port")
	listen := fs.Bool("listen", false, "start the test server (Server mode) immediately")
	noBrowser := fs.Bool("no-browser", false, "do not open any window (headless, use the printed URL)")
	browser := fs.Bool("browser", false, "open in the default browser instead of the app window")
	dir := fs.String("results", "", "folder for saved results (default: results next to the program)")
	_ = fs.Parse(args)
	err := gui.Run(gui.Options{
		WebAddr: *web, ServerPort: *port, StartServer: *listen,
		OpenBrowser: !*noBrowser, Browser: *browser, ResultsDir: *dir, Version: version,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func runServer(args []string) {
	fs := flag.NewFlagSet("server", flag.ExitOnError)
	port := fs.Int("p", engine.DefaultPort, "TCP control/data port and UDP probe port (UDP data uses port+1..port+N)")
	bind := fs.String("B", "", "bind to this local address")
	_ = fs.Parse(args)
	srv := &engine.Server{Port: *port, Bind: *bind, Logger: log.New(os.Stdout, "", log.LstdFlags)}
	go func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, os.Interrupt)
		<-ch
		srv.Close()
	}()
	if err := srv.ListenAndServe(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func runClient(args []string) {
	fs := flag.NewFlagSet("client", flag.ExitOnError)
	host := fs.String("c", "", "server address (required)")
	port := fs.Int("p", engine.DefaultPort, "server port")
	udp := fs.Bool("u", false, "use UDP (default TCP)")
	reverse := fs.Bool("R", false, "reverse: server sends, client receives")
	dur := fs.Float64("t", 10, "test duration in seconds")
	streams := fs.Int("P", 1, "parallel streams (use 4-8 for 10G/25G)")
	bw := fs.String("b", "", "target total bandwidth, e.g. 500M, 1G, 25G (TCP default unlimited, UDP default 1G)")
	length := fs.Int("l", 0, "TCP write size / UDP payload bytes (default TCP 128K, UDP 1460)")
	interval := fs.Float64("i", 1, "report interval in seconds")
	lat := fs.Int("L", 100, "latency probe interval in ms (0 disables)")
	perStream := fs.Bool("S", false, "show per-stream lines")
	jsonOut := fs.String("json", "", "write full results as JSON to this file")
	reportOut := fs.String("report", "", "write an HTML report to this file (open it and print to PDF)")
	csvOut := fs.String("csv", "", "write per-interval results as CSV to this file")
	_ = fs.Parse(args)
	if *host == "" {
		fs.Usage()
		os.Exit(2)
	}
	rate, err := engine.ParseBits(*bw)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(2)
	}
	p := engine.TestParams{
		Protocol: "tcp", Reverse: *reverse, DurationSec: *dur, Streams: *streams,
		Bandwidth: rate, Length: *length, IntervalSec: *interval, LatencyMs: *lat,
	}
	if *udp {
		p.Protocol = "udp"
	}
	if err := p.Normalize(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(2)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	dir := "client -> server"
	if p.Reverse {
		dir = "server -> client"
	}
	target := "unlimited"
	if p.Bandwidth > 0 {
		target = engine.FormatBits(float64(p.Bandwidth))
	}
	fmt.Printf("Connecting to %s:%d  %s %s, %d stream(s), %gs, target %s\n",
		*host, *port, strings.ToUpper(p.Protocol), dir, p.Streams, p.DurationSec, target)
	printHeader(p)

	res, err := engine.RunClient(ctx, *host, *port, p, func(iv engine.Interval) {
		if *perStream && len(iv.Streams) > 1 {
			for _, s := range iv.Streams {
				printRow(p, fmt.Sprintf("%3d", s.Stream), iv, s, false)
			}
		}
		printRow(p, "SUM", iv, iv.Sum, true)
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	printSummary(res)
	if *jsonOut != "" {
		b, _ := json.MarshalIndent(res, "", "  ")
		if err := os.WriteFile(*jsonOut, b, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "error writing json:", err)
			os.Exit(1)
		}
		fmt.Println("Results written to", *jsonOut)
	}
	if *reportOut != "" {
		if err := writeFile(*reportOut, func(f *os.File) error {
			return report.HTML(f, report.Input{ID: res.StartedAt.Format("20060102-150405"), Target: res.Server,
				Result: res, Version: version, Verdict: engine.Evaluate(res, engine.Criteria{})})
		}); err != nil {
			fmt.Fprintln(os.Stderr, "error writing report:", err)
			os.Exit(1)
		}
		fmt.Println("Report written to", *reportOut)
	}
	if *csvOut != "" {
		if err := writeFile(*csvOut, func(f *os.File) error { return report.CSV(f, res) }); err != nil {
			fmt.Fprintln(os.Stderr, "error writing csv:", err)
			os.Exit(1)
		}
		fmt.Println("CSV written to", *csvOut)
	}
}

func printHeader(p engine.TestParams) {
	if p.Protocol == "udp" {
		fmt.Printf("%-5s %-13s %12s %14s %10s %16s %9s %10s\n",
			"[ID]", "Interval", "Transfer", "Bandwidth", "Jitter", "Lost/Total", "Loss", "RTT avg")
	} else {
		fmt.Printf("%-5s %-13s %12s %14s %10s %10s\n", "[ID]", "Interval", "Transfer", "Bandwidth", "RTT avg", "RTT max")
	}
}

func printRow(p engine.TestParams, id string, iv engine.Interval, s engine.StreamInterval, withLatency bool) {
	rttAvg, rttMax := "-", "-"
	if withLatency && iv.Latency != nil && iv.Latency.Received > 0 {
		rttAvg = fmt.Sprintf("%.2f ms", iv.Latency.AvgMs)
		rttMax = fmt.Sprintf("%.2f ms", iv.Latency.MaxMs)
	}
	span := fmt.Sprintf("%5.2f-%5.2fs", iv.Start, iv.End)
	if p.Protocol == "udp" {
		fmt.Printf("[%-3s] %-13s %12s %14s %7.3f ms %16s %8.3f%% %10s\n",
			id, span, engine.FormatBytes(s.Bytes), engine.FormatBits(s.BitsPerSec), s.JitterMs,
			fmt.Sprintf("%d/%d", s.Lost, s.Packets+s.Lost), s.LossPct, rttAvg)
	} else {
		fmt.Printf("[%-3s] %-13s %12s %14s %10s %10s\n",
			id, span, engine.FormatBytes(s.Bytes), engine.FormatBits(s.BitsPerSec), rttAvg, rttMax)
	}
}

func printSummary(r *engine.Result) {
	s := r.Summary
	fmt.Println(strings.Repeat("-", 90))
	fmt.Printf("Summary  (%s, %s, duration %.2fs)\n", strings.ToUpper(r.Params.Protocol), r.Server, s.DurationSec)
	fmt.Printf("  Bandwidth : avg %s   min %s   max %s\n",
		engine.FormatBits(s.BitsPerSec), engine.FormatBits(s.MinBitsPerSec), engine.FormatBits(s.MaxBitsPerSec))
	fmt.Printf("  Transfer  : received %s  (sent %s)\n", engine.FormatBytes(s.Bytes), engine.FormatBytes(s.SentBytes))
	if r.Params.Protocol == "udp" {
		fmt.Printf("  Packets   : sent %d  received %d  lost %d (%.3f%%)  out-of-order %d\n",
			s.PacketsSent, s.PacketsReceived, s.Lost, s.LossPct, s.OutOfOrder)
		fmt.Printf("  Jitter    : %.3f ms\n", s.JitterMs)
	}
	if l := s.Latency; l != nil && l.Received > 0 {
		fmt.Printf("  Latency   : min %.2f  avg %.2f  p95 %.2f  max %.2f ms  jitter %.2f ms  probe loss %.1f%% (%d/%d)\n",
			l.MinMs, l.AvgMs, l.P95Ms, l.MaxMs, l.JitterMs, l.LossPct, l.Sent-l.Received, l.Sent)
	} else if l != nil {
		fmt.Printf("  Latency   : no probe replies (UDP port %s blocked?)\n", "probe")
	}
}

func writeFile(path string, fn func(*os.File) error) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := fn(f); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
