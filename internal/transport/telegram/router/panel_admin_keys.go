package router

import "gopkg.in/telebot.v3"

func (r *Router) handleAdminKeysPanel(c telebot.Context) error {
	if !isPrivateChat(c) {
		return r.editOrSendText(c, "请私聊我使用该功能。")
	}
	if !r.isAdminSender(c) {
		return r.editOrSendText(c, "无权限。")
	}
	if r.regAdmin == nil {
		return r.editOrSendText(c, "密钥管理尚未初始化。")
	}
	_ = c.Respond(&telebot.CallbackResponse{})
	r.tryRemoveReplyKeyboard(c)
	return r.sendKeyPanel(c)
}

func (r *Router) sendKeyPanel(c telebot.Context) error {
	return r.sendKeyPanelWithMessageID(c, 0)
}

func (r *Router) buildKeyMenu() *telebot.ReplyMarkup {
	menu := &telebot.ReplyMarkup{}
	menu.Inline(
		menu.Row(menu.Data("生成注册码", CbRegCreateCodes), menu.Data("生成续费码", CbRegCreateRenewCodes)),
		// “发放资格/统计”仍保留命令入口，但从面板按钮中移除（避免按钮过多）。
		menu.Row(menu.Data("导出未用", CbRegExport), menu.Data("销毁未用", CbRegWipe)),
		menu.Row(menu.Data("返回面板", CbAdminPanel)),
	)
	return menu
}

func (r *Router) sendKeyPanelWithMessageID(c telebot.Context, messageID int) error {
	msg := "🔑 密钥管理\n\n请选择一个操作："
	if messageID > 0 {
		if err := r.editByMessageIDAuto(c, messageID, msg, r.buildKeyMenu()); err == nil {
			return nil
		}
	}
	return r.editOrSendText(c, msg, r.buildKeyMenu())
}
