package main

import (
 "context"
 "flag"
 "fmt"
 "log"
 "net/http"
 "os"
 "os/signal"
 "strings"
 "syscall"
 "time"

 "github.com/ABHILASH9519/Corrective-Rag/internal/config"
 "github.com/ABHILASH9519/Corrective-Rag/internal/engine"
 "github.com/ABHILASH9519/Corrective-Rag/internal/server"
)
func main(){
 if len(os.Args)<2 {fmt.Println("crag serve | ask [--verbose] QUESTION | evaluate");os.Exit(2)}
 cfg:=config.Load()
 switch os.Args[1] {
 case "serve":
  e,err:=engine.New(cfg);if err!=nil{log.Fatal(err)}
  srv:=&http.Server{Addr:cfg.Address,Handler:server.New(e,cfg),ReadHeaderTimeout:5*time.Second,ReadTimeout:30*time.Second,WriteTimeout:90*time.Second,IdleTimeout:60*time.Second}
  ctx,stop:=signal.NotifyContext(context.Background(),os.Interrupt,syscall.SIGTERM);defer stop()
  go func(){<-ctx.Done();c,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel();_ = srv.Shutdown(c)}()
  log.Printf("Corrective RAG Go API listening on %s",cfg.Address);if err:=srv.ListenAndServe();err!=nil&&err!=http.ErrServerClosed{log.Fatal(err)}
 case "ask":
  fs:=flag.NewFlagSet("ask",flag.ExitOnError);verbose:=fs.Bool("verbose",false,"show retrieval trace");_ = fs.Parse(os.Args[2:])
  e,err:=engine.New(cfg);if err!=nil{log.Fatal(err)};r,err:=e.Ask(context.Background(),strings.Join(fs.Args()," "));if err!=nil{log.Fatal(err)}
  if *verbose{fmt.Printf("Verdict: %s\nPassages: %+v\n",r.Verdict,r.Passages)};fmt.Println(r.Answer)
 case "evaluate":
  e,err:=engine.New(cfg);if err!=nil{log.Fatal(err)};fmt.Println(e.Evaluate())
 default: log.Fatal("unknown command")
 }
}
