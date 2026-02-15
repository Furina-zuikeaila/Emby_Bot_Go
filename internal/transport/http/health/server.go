package health

import (
	"log"
	"net/http"
)

// Start 启动一个最小的健康检查 HTTP 服务。
//
// - 路径：GET /health → 200 OK + "ok"
// - 目的：容器编排（Docker/Compose/K8s）用来探测进程存活与就绪
// - 注意：失败时只写日志，不让健康服务异常影响主流程
func Start(addr string) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("健康检查服务异常：%v", err)
		}
	}()
	return srv
}
