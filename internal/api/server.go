package api

import (
	"context"
	"io/fs"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/store"
	trc "github.com/AceDarkknight/k8s-analyzer-agent/internal/trace"
)

type Server struct {
	traceStore store.TraceStore
	router     *gin.Engine
	webFS      fs.FS
}

func NewServer(traceStore store.TraceStore, webFS fs.FS) *Server {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(cors.New(cors.Config{
		AllowAllOrigins: true,
		AllowMethods:    []string{"GET", "OPTIONS"},
		AllowHeaders:    []string{"Origin", "Content-Type", "Authorization"},
		MaxAge:          12 * time.Hour,
	}))
	s := &Server{traceStore: traceStore, router: r, webFS: webFS}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return s.router
}

func (s *Server) routes() {
	s.router.GET("/health", s.handleHealth)
	s.router.GET("/metrics", gin.WrapH(promhttp.Handler()))

	api := s.router.Group("/api/v1")
	{
		api.GET("/stats", s.handleStats)
		api.GET("/tasks", s.handleTasks)
		api.GET("/tasks/:id", s.handleTaskDetail)
	}

	if s.webFS != nil {
		s.router.NoRoute(s.handleFrontend)
	}
}

func (s *Server) handleHealth(c *gin.Context) {
	writeJSON(c, http.StatusOK, CodeOK, map[string]string{"status": "ok"})
}

func (s *Server) handleTasks(c *gin.Context) {
	page := parseInt(c.Query("page"), 1)
	size := parseInt(c.Query("size"), 20)
	records, total, err := s.traceStore.ListTraces(c.Request.Context(), page, size)
	if err != nil {
		writeError(c, http.StatusInternalServerError, CodeStoreError)
		return
	}
	writeJSON(c, http.StatusOK, CodeOK, toTaskListData(records, total, page, size))
}

func (s *Server) handleStats(c *gin.Context) {
	stats, err := s.computeStats(c.Request.Context())
	if err != nil {
		writeError(c, http.StatusInternalServerError, CodeStoreError)
		return
	}
	writeJSON(c, http.StatusOK, CodeOK, stats)
}

func (s *Server) handleTaskDetail(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		writeError(c, http.StatusBadRequest, CodeBadRequest)
		return
	}
	trace, err := s.traceStore.GetTrace(c.Request.Context(), id)
	if err != nil {
		writeError(c, http.StatusNotFound, CodeNotFound)
		return
	}
	trace = trc.SanitizeTaskTrace(trace)
	writeJSON(c, http.StatusOK, CodeOK, toTaskTraceDTO(trace))
}

func (s *Server) handleFrontend(c *gin.Context) {
	if s.webFS == nil {
		c.Status(http.StatusNotFound)
		return
	}
	requestPath := strings.TrimPrefix(path.Clean(c.Request.URL.Path), "/")
	if requestPath == "" || requestPath == "." {
		requestPath = "index.html"
	}
	if _, err := fs.Stat(s.webFS, requestPath); err == nil {
		http.FileServer(http.FS(s.webFS)).ServeHTTP(c.Writer, c.Request)
		return
	}
	indexBytes, err := fs.ReadFile(s.webFS, "index.html")
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", indexBytes)
}

func (s *Server) computeStats(ctx context.Context) (*taskStatsData, error) {
	const pageSize = 500
	page := 1
	stats := &taskStatsData{}
	trendMap := map[string]*taskTrendPoint{}
	toolMap := map[string]*toolUsageRecord{}
	var totalDuration int64

	for {
		records, total, err := s.traceStore.ListTraces(ctx, page, pageSize)
		if err != nil {
			return nil, err
		}
		if len(records) == 0 {
			break
		}
		for _, rec := range records {
			stats.TotalTasks++
			status := normalizeStatus(rec.Status)
			if status == "success" {
				stats.SuccessTasks++
			} else {
				stats.FailedTasks++
			}
			stats.PromptTokens += rec.PromptTokens
			stats.CompletionTokens += rec.CompletionTokens
			stats.TotalTokens += rec.TotalTokens
			totalDuration += rec.TotalDurationMs
			day := rec.Timestamp
			if len(day) >= 10 {
				day = day[:10]
			}
			if trendMap[day] == nil {
				trendMap[day] = &taskTrendPoint{Date: day}
			}
			if status == "success" {
				trendMap[day].Success++
			} else {
				trendMap[day].Failed++
			}
			traceDetail, err := s.traceStore.GetTrace(ctx, rec.TaskID)
			if err == nil {
				for _, exec := range traceDetail.ToolExecutions {
					if toolMap[exec.ToolName] == nil {
						toolMap[exec.ToolName] = &toolUsageRecord{ToolName: exec.ToolName}
					}
					if exec.Success {
						toolMap[exec.ToolName].Success++
					} else {
						toolMap[exec.ToolName].Failed++
					}
				}
			}
		}
		if page*pageSize >= total {
			break
		}
		page++
	}

	if stats.TotalTasks > 0 {
		stats.SuccessRate = float64(stats.SuccessTasks) * 100 / float64(stats.TotalTasks)
		stats.AverageDurationMs = totalDuration / int64(stats.TotalTasks)
	}
	for _, point := range trendMap {
		stats.Trend = append(stats.Trend, *point)
	}
	sort.Slice(stats.Trend, func(i, j int) bool { return stats.Trend[i].Date < stats.Trend[j].Date })
	for _, usage := range toolMap {
		stats.ToolUsage = append(stats.ToolUsage, *usage)
	}
	sort.Slice(stats.ToolUsage, func(i, j int) bool { return stats.ToolUsage[i].ToolName < stats.ToolUsage[j].ToolName })
	return stats, nil
}

func parseInt(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}

func Start(ctx context.Context, addr string, traceStore store.TraceStore, webFS fs.FS) error {
	server := &http.Server{Addr: addr, Handler: NewServer(traceStore, webFS).Handler()}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	err := server.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
