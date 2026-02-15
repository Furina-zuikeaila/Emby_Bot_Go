// emby-bot-new 的进程入口（cmd/bot）。
//
// 这里尽量保持“薄”：只做启动/装配/优雅退出相关的事情，把业务逻辑留在 internal 下的分层中。
//
// 启动流程概览：
// 1) bootstrap.New：加载配置、连库、初始化 Emby/Telegram 客户端、构建 service/repository。
// 2) scheduler.New + Start：启动后台定时任务（检测/清理/审计推送等）。
// 3) telegram.NewRouter(...).Register：注册 Telegram 指令与回调路由。
// 4) bot.Start：开始 long polling 收取更新。
//
// 退出流程概览：
// - 捕获 SIGINT/SIGTERM → cancel 根 context → 停 scheduler/停 bot/停 health server。
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"emby-bot-new/internal/bootstrap"
	"emby-bot-new/internal/infrastructure/persistence/gormdb"
	"emby-bot-new/internal/infrastructure/tron"
	"emby-bot-new/internal/logging"
	"emby-bot-new/internal/transport/http/health"
	"emby-bot-new/internal/transport/scheduler"
	"emby-bot-new/internal/transport/telegram"
)

func main() {
	// 容器日志：统一时间戳格式，尽量输出中文信息，便于排查。
	log.SetFlags(log.LstdFlags)
	initFileLogger()

	if err := run(); err != nil {
		// 重要：不要直接打印 bootstrap 的原始错误链（可能包含 DSN/API Key/URL 等敏感信息）。
		log.Print(err.Error())
		os.Exit(1)
	}
}

func initFileLogger() {
	dir := logging.ResolveLogDir()
	w, err := logging.NewWeeklyFileWriter(dir, "bot")
	if err != nil {
		return
	}
	// 同时保留 stdout（便于容器/服务查看实时日志）。
	log.SetOutput(io.MultiWriter(os.Stdout, w))
}

func run() error {
	// signal.NotifyContext 会在收到信号时自动取消 ctx，避免手动 chan + cancel 的样板代码。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app, err := bootstrap.New()
	if err != nil {
		// 不返回原始 err，避免上层打印时泄露敏感信息。
		return fmt.Errorf("bootstrap failed (fingerprint=%s)", errorFingerprint(err))
	}

	sched := scheduler.New(
		app.Bot,
		app.UserRepo,
		app.SettingsRepo,
		app.InviteCodeRepo,
		app.EmbyClient,
		app.Config.Scheduler,
		app.Config.Govern,
		app.Config.Invite,
		app.Config.Telegram.OwnerID,
		app.Config.Telegram.AdminIDs,
	)
	sched.Start(ctx)

	var crowdfundRepo *gormdb.CrowdfundReceiptRepository
	var tronVerifier *tron.TronScanClient
	if app.Config.Crowdfund.Enabled {
		crowdfundRepo = gormdb.NewCrowdfundReceiptRepository(app.DB)
		var err error
		tronVerifier, err = tron.NewTronScanClient(tron.TronScanOptions{
			APIBase:       app.Config.Crowdfund.TronScanAPIBase,
			HTTPTimeout:   app.Config.Crowdfund.HTTPTimeout,
			SearchLimit:   app.Config.Crowdfund.SearchLimit,
			SearchMaxPage: app.Config.Crowdfund.SearchMaxPages,
		})
		if err != nil {
			return fmt.Errorf("init crowdfund verifier failed: %w", err)
		}
	}

	telegram.NewRouter(app.Registration, app.RegAdmin, app.Admin, app.Account, telegram.Options{
		OwnerID:       app.Config.Telegram.OwnerID,
		AdminIDs:      app.Config.Telegram.AdminIDs,
		Govern:        app.Config.Govern,
		Revoker:       sched,
		EmbyPublicURL: app.Config.Emby.PublicURL,

		Invite:                  app.Invite,
		InviteMinAccountAgeDays: app.Config.Invite.MinAccountAgeDays,
		InviteCooldownDays:      app.Config.Invite.CooldownDays,
		InviteReservationTTL:    app.Config.Invite.ReservationTTL,

		Crowdfund:     app.Config.Crowdfund,
		CrowdfundRepo: crowdfundRepo,
		TronVerifier:  tronVerifier,
	}).Register(app.Bot)

	healthSrv := health.Start(app.Config.HTTP.HealthAddr)

	go app.Bot.Start()
	log.Printf("机器人已启动：@%s", app.Bot.Me.Username)

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sched.Stop()
	app.Bot.Stop()
	_ = healthSrv.Shutdown(shutdownCtx)

	log.Println("已完成退出")
	return nil
}

// errorFingerprint 将错误字符串摘要为短指纹，用于日志关联而不泄露原始错误内容。
//
// 场景：
// - bootstrap.New 可能包含 DSN/API Key/URL 等敏感信息；日志只打印指纹，方便后续定位。
func errorFingerprint(err error) string {
	if err == nil {
		return "none"
	}
	sum := sha256.Sum256([]byte(err.Error()))
	// 短指纹用于关联排查，但不回显原始错误文本，避免泄露敏感信息。
	return hex.EncodeToString(sum[:])[:12]
}
