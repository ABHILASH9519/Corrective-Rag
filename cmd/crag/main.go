// Command crag is the Corrective RAG help desk: `crag ask "<question>"` and `crag serve`.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"crag/data"
	"crag/internal/config"
	"crag/internal/eval"
	"crag/internal/kb"
	"crag/internal/llm"
	"crag/internal/pipeline"
	"crag/internal/server"
	"crag/internal/web"
)

const version = "2.1.0"

const usage = `crag - Corrective RAG help desk

Usage:
  crag ask [-v] <question>     answer one question
  crag serve [-addr :8000]     run the web UI and HTTP API
  crag eval [-offline]         measure retrieval (offline) and routing (live) on the labelled set
  crag version`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		fmt.Println(usage)
		return errors.New("a command is required")
	}
	switch args[0] {
	case "ask":
		return cmdAsk(args[1:])
	case "serve":
		return cmdServe(args[1:])
	case "eval":
		return cmdEval(args[1:])
	case "version":
		fmt.Println("crag", version)
		return nil
	case "-h", "--help", "help":
		fmt.Println(usage)
		return nil
	}
	fmt.Println(usage)
	return fmt.Errorf("unknown command %q", args[0])
}

func build() (*pipeline.Pipeline, config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, cfg, err
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel})))
	key, err := cfg.RequireKey()
	if err != nil {
		return nil, cfg, err
	}
	index, err := kb.Load(cfg.KBPath, data.DefaultKnowledgeBase)
	if err != nil {
		return nil, cfg, err
	}
	slog.Info("knowledge base ready", "passages", index.Len())
	client := llm.NewGroq(key, cfg.BaseURL, cfg.Model, cfg.GradeMaxTokens, cfg.AnswerMaxTokens, cfg.MaxRetries)
	p := pipeline.New(index, client, web.NewDuckDuckGo(), pipeline.Options{
		TopK: cfg.TopK, WebResults: cfg.WebResults, HelpDesk: cfg.HelpDeskName, Hint: cfg.RewriteHint,
	})
	return p, cfg, nil
}

func cmdEval(args []string) error {
	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	offline := fs.Bool("offline", false, "retrieval check only: no API key or network needed")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))
	cases, err := eval.Load(data.EvaluationSet)
	if err != nil {
		return err
	}
	index, err := kb.Load(cfg.KBPath, data.DefaultKnowledgeBase)
	if err != nil {
		return err
	}
	eval.Retrieval(index, cases, cfg.TopK).Write(os.Stdout)
	if *offline {
		return nil
	}

	p, _, err := build()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Println()
	report, err := eval.Routing(ctx, p, cases, os.Stdout)
	if err != nil {
		return err
	}
	report.Write(os.Stdout)
	return nil
}

func cmdAsk(args []string) error {
	fs := flag.NewFlagSet("ask", flag.ContinueOnError)
	verbose := fs.Bool("v", false, "show grading and verdict")
	if err := fs.Parse(args); err != nil {
		return err
	}
	question := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if question == "" {
		return errors.New("please provide a question")
	}
	p, _, err := build()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	res, err := p.Ask(ctx, question)
	if err != nil {
		return err
	}
	if *verbose {
		fmt.Printf("Q: %s\n", res.Question)
		for _, ps := range res.Passages {
			fmt.Printf("  %.2f  %-10s  %s\n", ps.Similarity, ps.Label, ps.ID)
		}
		fmt.Printf("verdict: %s\n", res.Verdict)
		if res.WebQuery != "" {
			fmt.Printf("web query: %s\n", res.WebQuery)
		}
		fmt.Println()
	}
	fmt.Println(res.Answer)
	if len(res.Sources) > 0 {
		fmt.Println("\nSources: " + strings.Join(res.Sources, ", "))
	}
	return nil
}

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	addrFlag := fs.String("addr", "", "listen address (default from CRAG_ADDR, else :8000)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	p, cfg, err := build()
	if err != nil {
		return err
	}
	addr := cfg.Addr
	if *addrFlag != "" {
		addr = *addrFlag
	}
	srv := &http.Server{
		Addr: addr,
		Handler: server.New(p, cfg.Model, version, cfg.MaxQuestionLen, cfg.MaxConcurrent,
			server.RateLimit{PerMinute: cfg.RatePerMinute, TrustProxy: cfg.TrustProxy}).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	slog.Info("listening", "addr", addr)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		slog.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}
