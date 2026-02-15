package repo

import (
	"context"
	"errors"
	"strings"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

type CrowdfundReceiptRepository struct {
	db *gorm.DB
}

func NewCrowdfundReceiptRepository(db *gorm.DB) *CrowdfundReceiptRepository {
	return &CrowdfundReceiptRepository{db: db}
}

func (r *CrowdfundReceiptRepository) TryLock(ctx context.Context, txHash string, telegramID int64) (bool, error) {
	if r == nil || r.db == nil {
		return false, errors.New("nil db")
	}
	txHash = strings.TrimSpace(txHash)
	if txHash == "" {
		return false, nil
	}

	row := CrowdfundReceipt{
		TxHash:      txHash,
		TelegramID:  telegramID,
		Status:      "pending",
		InviteCode:  "",
		AmountQuant: "",
	}
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		var me *mysql.MySQLError
		if errors.As(err, &me) && me.Number == 1062 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (r *CrowdfundReceiptRepository) GetStatus(ctx context.Context, txHash string) (string, bool, error) {
	if r == nil || r.db == nil {
		return "", false, errors.New("nil db")
	}
	txHash = strings.TrimSpace(txHash)
	if txHash == "" {
		return "", false, nil
	}
	var row CrowdfundReceipt
	if err := r.db.WithContext(ctx).First(&row, "tx_hash = ?", txHash).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", false, nil
		}
		return "", false, err
	}
	return strings.TrimSpace(row.Status), true, nil
}

func (r *CrowdfundReceiptRepository) Release(ctx context.Context, txHash string) error {
	if r == nil || r.db == nil {
		return errors.New("nil db")
	}
	txHash = strings.TrimSpace(txHash)
	if txHash == "" {
		return nil
	}
	return r.db.WithContext(ctx).Delete(&CrowdfundReceipt{}, "tx_hash = ? AND status = ?", txHash, "pending").Error
}

func (r *CrowdfundReceiptRepository) MarkIssued(ctx context.Context, txHash string, toAddress string, contractAddress string, tokenSymbol string, amountQuant string, inviteCode string) error {
	if r == nil || r.db == nil {
		return errors.New("nil db")
	}
	txHash = strings.TrimSpace(txHash)
	if txHash == "" {
		return nil
	}
	return r.db.WithContext(ctx).
		Model(&CrowdfundReceipt{}).
		Where("tx_hash = ?", txHash).
		Updates(map[string]any{
			"to_address":       strings.TrimSpace(toAddress),
			"contract_address": strings.TrimSpace(contractAddress),
			"token_symbol":     strings.TrimSpace(tokenSymbol),
			"amount_quant":     strings.TrimSpace(amountQuant),
			"invite_code":      strings.TrimSpace(inviteCode),
			"status":           "issued",
		}).Error
}

func (r *CrowdfundReceiptRepository) MarkReceived(ctx context.Context, txHash string, toAddress string, contractAddress string, tokenSymbol string, amountQuant string) error {
	if r == nil || r.db == nil {
		return errors.New("nil db")
	}
	txHash = strings.TrimSpace(txHash)
	if txHash == "" {
		return nil
	}
	return r.db.WithContext(ctx).
		Model(&CrowdfundReceipt{}).
		Where("tx_hash = ?", txHash).
		Updates(map[string]any{
			"to_address":       strings.TrimSpace(toAddress),
			"contract_address": strings.TrimSpace(contractAddress),
			"token_symbol":     strings.TrimSpace(tokenSymbol),
			"amount_quant":     strings.TrimSpace(amountQuant),
			"invite_code":      "",
			"status":           "received",
		}).Error
}

func (r *CrowdfundReceiptRepository) IsIssued(ctx context.Context, txHash string) (bool, error) {
	if r == nil || r.db == nil {
		return false, errors.New("nil db")
	}
	txHash = strings.TrimSpace(txHash)
	if txHash == "" {
		return false, nil
	}
	var row CrowdfundReceipt
	err := r.db.WithContext(ctx).First(&row, "tx_hash = ?", txHash).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	return strings.EqualFold(strings.TrimSpace(row.Status), "issued"), nil
}
