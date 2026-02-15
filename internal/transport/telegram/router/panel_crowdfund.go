package router

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"emby-bot-new/internal/infrastructure/tron"

	"gopkg.in/telebot.v3"
)

type crowdfundReceiptRepoLegacy interface {
	TryLock(ctx context.Context, txHash string, telegramID int64) (bool, error)
	GetStatus(ctx context.Context, txHash string) (string, bool, error)
	Release(ctx context.Context, txHash string) error
	MarkIssued(ctx context.Context, txHash string, toAddress string, contractAddress string, tokenSymbol string, amountQuant string, inviteCode string) error
	MarkReceived(ctx context.Context, txHash string, toAddress string, contractAddress string, tokenSymbol string, amountQuant string) error
	IsIssued(ctx context.Context, txHash string) (bool, error)
}

type trc20VerifierLegacy interface {
	VerifyTRC20Transfer(ctx context.Context, txHash string, expect tron.Expectation) (tron.TRC20Transfer, error)
}

func crowdfundThanksLegacy() string {
	return "✅ 感谢你的支持！"
}

func (r *Router) handleCrowdfundLegacy(c telebot.Context) error {
	if c == nil || c.Sender() == nil {
		return nil
	}
	if !isPrivateChat(c) {
		return c.Send("请在私聊中使用该功能。")
	}
	if !r.crowdfund.Enabled {
		return r.editOrSendText(c, "当前未开启发电功能。", r.mainPanelMenu(c))
	}
	if r.crowdfundRepo == nil || r.tronVerifier == nil || r.regAdmin == nil {
		return r.editOrSendText(c, "发电功能未初始化，请联系管理员。", r.mainPanelMenu(c))
	}

	r.state.Clear(c.Sender().ID)
	msg := r.crowdfundPromptLegacy()
	return r.upsertUserConvoMessage(c, convoCrowdfundTxHash, convoSession{}, nil, msg, telebot.ModeMarkdown, r.cancelMenu())
}

func (r *Router) crowdfundPromptLegacy() string {
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
		"您捐赠的钱将用于维护Emby服务器和开发机器人，感谢您的支持",
		"点击“取消”返回。",
	}, "\n")
}

func (r *Router) handleCrowdfundTxHashInputLegacy(c telebot.Context, input string) error {
	if c == nil || c.Sender() == nil {
		return nil
	}
	sess, _ := r.state.Get(c.Sender().ID)

	txHash, ok := normalizeTxHashLegacy(input)
	if !ok {
		return r.editWithSessionMessage(c, sess, "交易哈希格式不正确，请发送 64 位十六进制 TxID。\n\n点击“取消”返回。", telebot.ModeMarkdown, r.cancelMenu())
	}
	_ = c.Delete()

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	locked, err := r.crowdfundRepo.TryLock(ctx, txHash, c.Sender().ID)
	if err != nil {
		return r.editWithSessionMessage(c, sess, "系统繁忙，请稍后再试。\n\n点击“取消”返回。", r.cancelMenu())
	}
	if !locked {
		if st, ok, err := r.crowdfundRepo.GetStatus(ctx, txHash); err == nil && ok {
			switch strings.ToLower(strings.TrimSpace(st)) {
			case "issued":
				return r.editWithSessionMessage(c, sess, "✅ 感谢你的支持！\n\n这笔交易已完成兑换，如需帮助请联系管理员。\n\n点击“取消”返回。", r.cancelMenu())
			case "received":
				symbol := strings.TrimSpace(r.crowdfund.TRC20Symbol)
				if symbol == "" {
					symbol = "TRC20"
				}
				minAmount := strings.TrimSpace(r.crowdfund.MinAmount)
				if minAmount == "" || minAmount == "0" {
					minAmount = "0"
				}
				return r.editWithSessionMessage(c, sess, fmt.Sprintf("✅ 感谢你的支持！\n\n这笔交易已确认发电。\n仅当金额 ≥ %s %s 才会自动发放注册码；本次未达到该条件。\n如需注册码或有疑问，请联系管理员协助处理。", safeInlineCode(minAmount), safeInlineCode(symbol)), r.mainPanelMenu(c))
			default:
				return r.editWithSessionMessage(c, sess, "🙏 感谢你的支持！\n\n该交易正在处理中，请稍后再试。\n\n点击“取消”返回。", r.cancelMenu())
			}
		}
		issued, _ := r.crowdfundRepo.IsIssued(ctx, txHash)
		if issued {
			return r.editWithSessionMessage(c, sess, "✅ 感谢你的支持！\n\n这笔交易已完成兑换，如需帮助请联系管理员。\n\n点击“取消”返回。", r.cancelMenu())
		}
		return r.editWithSessionMessage(c, sess, "🙏 感谢你的支持！\n\n该交易正在处理中，请稍后再试。\n\n点击“取消”返回。", r.cancelMenu())
	}
	defer func() { _ = r.crowdfundRepo.Release(context.Background(), txHash) }()

	var minQuant *big.Int
	minAmount := strings.TrimSpace(r.crowdfund.MinAmount)
	if minAmount != "" && minAmount != "0" {
		q, err := tron.ParseDecimalToQuant(minAmount, r.crowdfund.TRC20Decimals)
		if err != nil {
			return r.editWithSessionMessage(c, sess, "发电配置异常（金额格式），请联系管理员。", r.mainPanelMenu(c))
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
		if errors.Is(err, tron.ErrTransferNotFound) || errors.Is(err, tron.ErrTransferNotConfirmed) {
			return r.editWithSessionMessage(c, sess, "🙏 感谢你的支持！\n\n暂未查询到该交易或交易尚未确认，请稍后再试。\n\n点击“取消”返回。", r.cancelMenu())
		}
		if errors.Is(err, tron.ErrTransferMismatch) {
			return r.editWithSessionMessage(c, sess, "🙏 感谢你的支持！\n\n我没有在指定收款地址/币种网络下匹配到该交易。\n可能是：发错网络/发错地址/粘贴的 TxID 不对应。\n如你确认无误，请联系管理员协助核对。", r.mainPanelMenu(c))
		}
		return r.editWithSessionMessage(c, sess, "🙏 感谢你的支持！\n\n交易校验暂时失败，请稍后再试；如多次失败请联系管理员。", r.mainPanelMenu(c))
	}

	issueCtx, issueCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer issueCancel()

	if minQuant != nil && tr.Quant != nil && tr.Quant.Cmp(minQuant) < 0 {
		tokenSymbol := strings.TrimSpace(tr.TokenSymbol)
		if tokenSymbol == "" {
			tokenSymbol = strings.TrimSpace(r.crowdfund.TRC20Symbol)
		}
		if err := r.crowdfundRepo.MarkReceived(issueCtx, txHash, tr.ToAddress, tr.ContractAddress, tokenSymbol, tr.Quant.String()); err != nil {
			return r.editWithSessionMessage(c, sess, "✅ 感谢你的支持！\n\n已确认发电，但记录写入失败，请联系管理员协助核对。", r.mainPanelMenu(c))
		}
		r.state.Clear(c.Sender().ID)
		symbol := strings.TrimSpace(r.crowdfund.TRC20Symbol)
		if symbol == "" {
			symbol = "TRC20"
		}
		minAmount := strings.TrimSpace(r.crowdfund.MinAmount)
		if minAmount == "" || minAmount == "0" {
			minAmount = "0"
		}
		return r.editWithSessionMessage(c, sess, fmt.Sprintf("✅ 感谢你的支持！\n\n已确认发电。\n仅当金额 `≥ %s` %s 才会自动发放注册码；本次未达到该条件。\n如需注册码或有疑问，请联系管理员协助处理。", safeInlineCode(minAmount), safeInlineCode(symbol)), telebot.ModeMarkdown, r.mainPanelMenu(c))
	}

	codes, err := r.regAdmin.CreateCodes(issueCtx, r.ownerID, 0, 1, false)
	if err != nil || len(codes) == 0 {
		return r.editWithSessionMessage(c, sess, "发放失败，请联系管理员。", r.mainPanelMenu(c))
	}
	code := codes[0]

	tokenSymbol := strings.TrimSpace(tr.TokenSymbol)
	if tokenSymbol == "" {
		tokenSymbol = strings.TrimSpace(r.crowdfund.TRC20Symbol)
	}
	if err := r.crowdfundRepo.MarkIssued(issueCtx, txHash, tr.ToAddress, tr.ContractAddress, tokenSymbol, tr.Quant.String(), code); err != nil {
		return r.editWithSessionMessage(c, sess, "✅ 感谢你的支持！\n\n注册码已生成，但写入记录失败，请联系管理员协助核对。", r.mainPanelMenu(c))
	}

	r.state.Clear(c.Sender().ID)
	msg := strings.Join([]string{
		"✅ 感谢你的支持！\n\n交易校验通过，已发放邀请码：",
		fmt.Sprintf("`%s`", safeInlineCode(code)),
		"",
		"你可以把它发给需要注册的人使用。",
	}, "\n")
	return r.editWithSessionMessage(c, sess, msg, telebot.ModeMarkdown, r.mainPanelMenu(c))
}

func normalizeTxHashLegacy(input string) (string, bool) {
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
