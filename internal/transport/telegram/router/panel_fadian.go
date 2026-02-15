package router

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"emby-bot-new/internal/infrastructure/tron"

	"gopkg.in/telebot.v3"
)

type crowdfundReceiptRepo interface {
	TryLock(ctx context.Context, txHash string, telegramID int64) (bool, error)
	GetStatus(ctx context.Context, txHash string) (string, bool, error)
	Release(ctx context.Context, txHash string) error
	MarkIssued(ctx context.Context, txHash string, toAddress string, contractAddress string, tokenSymbol string, amountQuant string, inviteCode string) error
	MarkReceived(ctx context.Context, txHash string, toAddress string, contractAddress string, tokenSymbol string, amountQuant string) error
	IsIssued(ctx context.Context, txHash string) (bool, error)
}

type trc20Verifier interface {
	VerifyTRC20Transfer(ctx context.Context, txHash string, expect tron.Expectation) (tron.TRC20Transfer, error)
}

func crowdfundThanks() string {
	return "✅ 感谢你的支持！"
}

func (r *Router) handleCrowdfund(c telebot.Context) error {
	if c == nil || c.Sender() == nil {
		return nil
	}
	if !isPrivateChat(c) {
		return c.Send("请在私聊中使用该功能。")
	}
	if !r.crowdfund.Enabled {
		return r.editOrSendText(c, crowdfundThanks(), r.mainPanelMenu(c))
	}
	if r.crowdfundRepo == nil || r.tronVerifier == nil || r.regAdmin == nil {
		return r.editOrSendText(c, crowdfundThanks(), r.mainPanelMenu(c))
	}

	r.state.Clear(c.Sender().ID)
	return r.upsertUserConvoMessage(c, convoCrowdfundTxHash, convoSession{}, nil, r.crowdfundPrompt(), telebot.ModeMarkdown, r.cancelMenu())
}

func (r *Router) crowdfundPrompt() string {
	symbol := strings.TrimSpace(r.crowdfund.TRC20Symbol)
	if symbol == "" {
		symbol = "TRC20"
	}
	minAmount := strings.TrimSpace(r.crowdfund.MinAmount)
	amountLine := "任意金额都算发电（>0）"
	if minAmount != "" && minAmount != "0" {
		amountLine = fmt.Sprintf("任意金额都算发电；仅当金额 `≥ %s` %s 才会自动发放注册码", safeInlineCode(minAmount), safeInlineCode(symbol))
	}

	return strings.Join([]string{
		"⚡ 发电（TRC20）",
		"",
		fmt.Sprintf("收款地址：`%s`", safeInlineCode(strings.TrimSpace(r.crowdfund.TRC20Address))),
		fmt.Sprintf("币种：`%s(TRC20)`", safeInlineCode(symbol)),
		amountLine,
		"",
		"请完成转账后，把交易哈希（TxID，64 位十六进制）发给我。",
		"例如：`a1b2c3...`",
		"",
		"点击“取消”返回。",
	}, "\n")
}

func (r *Router) handleCrowdfundTxHashInput(c telebot.Context, input string) error {
	if c == nil || c.Sender() == nil {
		return nil
	}
	sess, _ := r.state.Get(c.Sender().ID)

	txHash, ok := normalizeTxHash(input)
	if !ok {
		return r.editWithSessionMessage(c, sess, "交易哈希格式不正确，请发送 64 位十六进制 TxID。\n\n点击“取消”返回。", telebot.ModeMarkdown, r.cancelMenu())
	}
	_ = c.Delete()

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	locked, err := r.crowdfundRepo.TryLock(ctx, txHash, c.Sender().ID)
	if err != nil || !locked {
		r.state.Clear(c.Sender().ID)
		return r.editWithSessionMessage(c, sess, crowdfundThanks(), r.mainPanelMenu(c))
	}
	defer func() { _ = r.crowdfundRepo.Release(context.Background(), txHash) }()

	var minQuant *big.Int
	minAmount := strings.TrimSpace(r.crowdfund.MinAmount)
	if minAmount != "" && minAmount != "0" {
		q, err := tron.ParseDecimalToQuant(minAmount, r.crowdfund.TRC20Decimals)
		if err != nil {
			r.state.Clear(c.Sender().ID)
			return r.editWithSessionMessage(c, sess, crowdfundThanks(), r.mainPanelMenu(c))
		}
		minQuant = q
	}

	tr, err := r.tronVerifier.VerifyTRC20Transfer(ctx, txHash, tron.Expectation{
		ToAddress:       r.crowdfund.TRC20Address,
		ContractAddress: r.crowdfund.TRC20Contract,
		Decimals:        r.crowdfund.TRC20Decimals,
		ExpectedQuant:   nil,
		RequireConfirm:  true,
	})
	if err != nil {
		// 只表示感谢，不提示原因（避免伤害捐赠人）。
		r.state.Clear(c.Sender().ID)
		return r.editWithSessionMessage(c, sess, crowdfundThanks(), r.mainPanelMenu(c))
	}

	issueCtx, issueCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer issueCancel()

	// 未达到门槛：记录为 received（防止重复提交），并仅表示感谢。
	if minQuant != nil && tr.Quant != nil && tr.Quant.Cmp(minQuant) < 0 {
		tokenSymbol := strings.TrimSpace(tr.TokenSymbol)
		if tokenSymbol == "" {
			tokenSymbol = strings.TrimSpace(r.crowdfund.TRC20Symbol)
		}
		_ = r.crowdfundRepo.MarkReceived(issueCtx, txHash, tr.ToAddress, tr.ContractAddress, tokenSymbol, tr.Quant.String())
		r.state.Clear(c.Sender().ID)
		msg := crowdfundThanks()
		if minAmount := strings.TrimSpace(r.crowdfund.MinAmount); minAmount != "" && minAmount != "0" {
			symbol := strings.TrimSpace(r.crowdfund.TRC20Symbol)
			if symbol == "" {
				symbol = "TRC20"
			}
			msg = fmt.Sprintf("%s\n\n已确认发电。\n仅当金额 `≥ %s` %s 才会自动发放注册码；本次未达到该条件。", msg, safeInlineCode(minAmount), safeInlineCode(symbol))
		}
		return r.editWithSessionMessage(c, sess, msg, telebot.ModeMarkdown, r.mainPanelMenu(c))
	}

	// 达到门槛：直接发放注册码（0 天）。
	codes, err := r.regAdmin.CreateCodes(issueCtx, r.ownerID, 0, 1, false)
	if err != nil || len(codes) == 0 {
		r.state.Clear(c.Sender().ID)
		return r.editWithSessionMessage(c, sess, crowdfundThanks(), r.mainPanelMenu(c))
	}
	code := codes[0]

	tokenSymbol := strings.TrimSpace(tr.TokenSymbol)
	if tokenSymbol == "" {
		tokenSymbol = strings.TrimSpace(r.crowdfund.TRC20Symbol)
	}
	if err := r.crowdfundRepo.MarkIssued(issueCtx, txHash, tr.ToAddress, tr.ContractAddress, tokenSymbol, tr.Quant.String(), code); err != nil {
		// 记录失败时不返回邀请码，避免因重启导致重复发码。
		r.state.Clear(c.Sender().ID)
		return r.editWithSessionMessage(c, sess, crowdfundThanks(), r.mainPanelMenu(c))
	}

	r.state.Clear(c.Sender().ID)
	return r.editWithSessionMessage(c, sess, fmt.Sprintf("%s\n邀请码：`%s`", crowdfundThanks(), safeInlineCode(code)), telebot.ModeMarkdown, r.mainPanelMenu(c))
}

func normalizeTxHash(input string) (string, bool) {
	s := strings.TrimSpace(input)
	s = strings.TrimPrefix(s, "0x")
	s = strings.TrimPrefix(s, "0X")
	if len(s) != 64 {
		return "", false
	}
	for _, ch := range s {
		switch {
		case ch >= '0' && ch <= '9':
		case ch >= 'a' && ch <= 'f':
		case ch >= 'A' && ch <= 'F':
		default:
			return "", false
		}
	}
	return strings.ToLower(s), true
}
