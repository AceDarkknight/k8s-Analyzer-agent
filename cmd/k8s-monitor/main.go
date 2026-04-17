package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/api"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/config"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/logger"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/store"
	webui "github.com/AceDarkknight/k8s-analyzer-agent/web"
)

func main() {
	configPath := flag.String("config", "configs/config.yaml", "配置文件路径")
	port := flag.Int("port", 0, "监听端口，默认读取配置")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "加载配置失败: %v\n", err)
		os.Exit(1)
	}
	if err := logger.Init(&logger.LogConfig{Level: cfg.Log.Level, FilePath: cfg.Log.FilePath, MaxSizeMB: cfg.Log.MaxSizeMB, MaxBackups: cfg.Log.MaxBackups}); err != nil {
		fmt.Fprintf(os.Stderr, "初始化日志失败: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	traceStore, err := store.NewFileTraceStore(cfg.Monitor.TraceDir)
	if err != nil {
		logger.Fatal("TraceStore 初始化失败", logger.Err(err))
	}
	defer traceStore.Close()

	listenPort := cfg.Monitor.APIPort
	if *port > 0 {
		listenPort = *port
	}
	addr := fmt.Sprintf(":%d", listenPort)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	logger.Info("k8s-monitor 启动", logger.String("addr", addr), logger.String("trace_dir", cfg.Monitor.TraceDir))
	if err := api.Start(ctx, addr, traceStore, webui.DistFS()); err != nil {
		logger.Fatal("k8s-monitor 启动失败", logger.Err(err))
	}
}
