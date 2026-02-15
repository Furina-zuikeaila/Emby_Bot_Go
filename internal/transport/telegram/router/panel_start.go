package router

import (
	"os"
	"strings"

	"gopkg.in/telebot.v3"
)

// sendStartPage 发送用户侧“主面板”并记录锚点消息，后续导航优先编辑该锚点，避免出现多套菜单。
func (r *Router) sendStartPage(c telebot.Context) error {
	if c.Sender() == nil {
		return nil
	}
	if c.Chat() == nil || c.Bot() == nil {
		return nil
	}

	isAdmin := r.isAdminSender(c)
	isRegistered := false
	if r.reg != nil {
		ctx, cancel := bgCtxWithTimeout(timeout5s)
		defer cancel()
		if account, err := r.reg.Me(ctx, c.Sender().ID); err == nil && account != nil && account.EmbyUserID != "" {
			isRegistered = true
		}
	}

	menu := r.startPageMenu(isRegistered, isAdmin)
	caption := startCaption(c.Sender())

	raw := strings.TrimSpace(os.Getenv("TG_START_IMAGE_URL"))
	if raw == "" {
		sent, err := c.Bot().Send(c.Chat(), caption, menu)
		if err != nil {
			return err
		}
		r.ui.Set(c.Sender().ID, sent.ID, isMediaMessage(sent))
		return nil
	}

	photo := &telebot.Photo{Caption: caption}
	switch {
	case strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://"):
		photo.File = telebot.FromURL(raw)
	default:
		photo.File = telebot.File{FileID: raw}
	}

	sent, err := c.Bot().Send(c.Chat(), photo, menu)
	if err != nil {
		return err
	}
	r.ui.Set(c.Sender().ID, sent.ID, isMediaMessage(sent))
	return nil
}

// updateStartPage 尝试编辑主面板锚点消息；若锚点不存在/编辑失败，则回退为重新发送。
func (r *Router) updateStartPage(c telebot.Context) error {
	if c == nil || c.Sender() == nil {
		return nil
	}

	isAdmin := r.isAdminSender(c)
	isRegistered := false
	if r.reg != nil {
		ctx, cancel := bgCtxWithTimeout(timeout5s)
		defer cancel()
		if account, err := r.reg.Me(ctx, c.Sender().ID); err == nil && account != nil && account.EmbyUserID != "" {
			isRegistered = true
		}
	}

	caption := startCaption(c.Sender())
	menu := r.startPageMenu(isRegistered, isAdmin)

	// 优先更新“主面板锚点消息”，避免主菜单在不同消息之间漂移，导致出现多套面板。
	if anchor, ok := r.ui.Get(c.Sender().ID); ok {
		var err error
		if anchor.IsMedia {
			err = r.editCaptionByMessageID(c, anchor.MessageID, caption, menu)
		} else {
			err = r.editByMessageID(c, anchor.MessageID, caption, menu)
		}
		if err == nil {
			return nil
		}
	}
	return r.sendStartPage(c)
}

// mainPanelMenu 统一用户侧“主面板”的按钮（避免出现旧的多套主菜单）。
func (r *Router) mainPanelMenu(c telebot.Context) *telebot.ReplyMarkup {
	if c == nil || c.Sender() == nil {
		return &telebot.ReplyMarkup{}
	}

	isAdmin := r.isAdminSender(c)
	isRegistered := r.isRegisteredUser(c.Sender())
	return r.startPageMenu(isRegistered, isAdmin)
}

func (r *Router) startPageMenu(isRegistered bool, isAdmin bool) *telebot.ReplyMarkup {
	menu := &telebot.ReplyMarkup{}
	var rows []telebot.Row
	if isRegistered {
		// 注册后允许查看“服务器信息”（例如线路/在线人数）。
		rows = append(rows, menu.Row(
			menu.Data(StartBtnUserPanel, CbUserPanel),
			menu.Data(StartBtnServer, CbServerInfo),
		))
	} else {
		// 未注册用户不允许查看服务器信息（不展示入口）。
		if r != nil && r.crowdfund.Enabled {
			// 把“发电”放在“注册 Emby”旁边，作为同一行两个入口。
			rows = append(rows, menu.Row(
				menu.Data(StartBtnRegister, CbRegister),
				menu.Data(StartBtnCrowdfund, CbCrowdfund),
			))
		} else {
			rows = append(rows, menu.Row(menu.Data(StartBtnRegister, CbRegister)))
		}
	}
	// 已注册用户也允许“发电”（不影响注册/权限体系）。
	if isRegistered && r != nil && r.crowdfund.Enabled {
		rows = append(rows, menu.Row(menu.Data(StartBtnCrowdfund, CbCrowdfund)))
	}
	if isAdmin {
		rows = append(rows, menu.Row(menu.Data(StartBtnAdminPanel, CbAdminPanel)))
	}
	menu.Inline(rows...)
	return menu
}
