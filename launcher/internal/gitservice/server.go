package gitservice

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
)

const DefaultPort = "18080"

// Server Git HTTP服务器
type Server struct {
	port    string
	service *GitService
	server  *http.Server
	ctx     context.Context
	cancel  context.CancelFunc
}

// NewServer 创建HTTP服务器
func NewServer(port string) *Server {
	if port == "" {
		port = DefaultPort
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Server{
		port:    port,
		service: NewGitService(""),
		ctx:     ctx,
		cancel:  cancel,
	}
}

// SetProjectDir 设置项目目录
func (s *Server) SetProjectDir(projectDir string) {
	s.service.SetProjectDir(projectDir)
}

// Start 启动HTTP服务器
func (s *Server) Start() error {
	mux := http.NewServeMux()

	// 注册路由
	mux.HandleFunc("/api/checkpoints/status", s.handleStatus)
	mux.HandleFunc("/api/checkpoints/list", s.handleList)
	mux.HandleFunc("/api/checkpoints/save", s.handleSave)
	mux.HandleFunc("/api/checkpoints/restore", s.handleRestore)
	mux.HandleFunc("/api/checkpoints/diff/", s.handleDiff)
	mux.HandleFunc("/api/checkpoints/working-diff/", s.handleWorkingDiff)

	// 初始化Git仓库
	mux.HandleFunc("/api/checkpoints/init", s.handleInit)

	// 健康检查
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	s.server = &http.Server{
		Addr:    ":" + s.port,
		Handler: corsMiddleware(mux),
	}

	log.Printf("Git服务启动在端口 %s", s.port)

	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Git服务错误: %v", err)
		}
	}()

	return nil
}

// Stop 停止HTTP服务器
func (s *Server) Stop() error {
	s.cancel()
	if s.server != nil {
		return s.server.Shutdown(context.Background())
	}
	return nil
}

// corsMiddleware CORS中间件
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// writeJSON 写入JSON响应
func writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// writeError 写入错误响应
func writeError(w http.ResponseWriter, message string, status int) {
	w.WriteHeader(status)
	writeJSON(w, map[string]interface{}{
		"success": false,
		"message": message,
	})
}

// handleStatus 处理状态请求
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}

	status, err := s.service.GetStatus()
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, status)
}

// handleList 处理列表请求
func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}

	checkpoints, err := s.service.ListCheckpoints()
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]interface{}{
		"success":     true,
		"checkpoints": checkpoints,
		"count":       len(checkpoints),
	})
}

// handleSave 处理保存请求
func (s *Server) handleSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}

	var req SaveCheckpointRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// 如果请求体为空，使用空消息
		req.Message = ""
	}

	result, err := s.service.SaveCheckpoint(&req)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, result)
}

// handleRestore 处理恢复请求
func (s *Server) handleRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}

	var req RestoreCheckpointRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "无效的请求体", http.StatusBadRequest)
		return
	}

	if req.CommitHash == "" {
		writeError(w, "缺少commit_hash参数", http.StatusBadRequest)
		return
	}

	result, err := s.service.RestoreCheckpoint(&req)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, result)
}

// handleDiff 处理差异请求
func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}

	// 从URL中提取commit_hash
	// URL格式: /api/checkpoints/diff/{commit_hash}
	prefix := "/api/checkpoints/diff/"
	commitHash := strings.TrimPrefix(r.URL.Path, prefix)

	if commitHash == "" {
		writeError(w, "缺少commit_hash参数", http.StatusBadRequest)
		return
	}

	result, err := s.service.GetCheckpointDiff(commitHash)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, result)
}

// handleWorkingDiff 处理工作区差异请求
func (s *Server) handleWorkingDiff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}

	// 从URL中提取file_path
	// URL格式: /api/checkpoints/working-diff/{file_path}
	prefix := "/api/checkpoints/working-diff/"
	filePath := strings.TrimPrefix(r.URL.Path, prefix)

	if filePath == "" {
		writeError(w, "缺少file_path参数", http.StatusBadRequest)
		return
	}

	// URL解码
	filePath, err := decodePath(filePath)
	if err != nil {
		writeError(w, "无效的文件路径", http.StatusBadRequest)
		return
	}

	result, err := s.service.GetWorkingDiff(filePath)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, result)
}

// decodePath 解码URL路径
func decodePath(path string) (string, error) {
	// 简单的URL解码处理
	path = strings.ReplaceAll(path, "%2F", "/")
	path = strings.ReplaceAll(path, "%5C", "/")
	return path, nil
}

// handleInit 处理初始化请求
func (s *Server) handleInit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}

	result, err := s.service.InitRepo()
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, result)
}

// GetAddress 获取服务器地址
func (s *Server) GetAddress() string {
	return fmt.Sprintf("http://localhost:%s", s.port)
}
