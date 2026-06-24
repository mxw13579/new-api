package model

import (
	"errors"
	"fmt"
	"math"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TopUp struct {
	Id              int     `json:"id"`
	UserId          int     `json:"user_id" gorm:"index"`
	Amount          int64   `json:"amount"`
	Money           float64 `json:"money"`
	TradeNo         string  `json:"trade_no" gorm:"unique;type:varchar(255);index"`
	PaymentMethod   string  `json:"payment_method" gorm:"type:varchar(50)"`
	PaymentProvider string  `json:"payment_provider" gorm:"type:varchar(50);default:''"`
	CreateTime      int64   `json:"create_time"`
	CompleteTime    int64   `json:"complete_time"`
	Status          string  `json:"status"`
}

const (
	PaymentMethodStripe       = "stripe"
	PaymentMethodCreem        = "creem"
	PaymentMethodWaffo        = "waffo"
	PaymentMethodWaffoPancake = "waffo_pancake"
	PaymentMethodBalance      = "balance"
)

const (
	PaymentProviderEpay         = "epay"
	PaymentProviderStripe       = "stripe"
	PaymentProviderCreem        = "creem"
	PaymentProviderWaffo        = "waffo"
	PaymentProviderWaffoPancake = "waffo_pancake"
	PaymentProviderBalance      = "balance"
)

var (
	ErrPaymentMethodMismatch = errors.New("payment method mismatch")
	ErrTopUpNotFound         = errors.New("topup not found")
	ErrTopUpStatusInvalid    = errors.New("topup status invalid")
	ErrTopUpRebateConflict   = errors.New("topup rebate idempotency conflict")
)

type WalletTopUpCreditResult struct {
	UserId        int
	TradeNo       string
	PaymentMethod string
	Provider      string
	Money         float64
	QuotaToAdd    int
	RebateQuota   int
	InviterId     int
}

var walletTopUpPaymentProviders = map[string]bool{
	PaymentProviderEpay:         true,
	PaymentProviderStripe:       true,
	PaymentProviderCreem:        true,
	PaymentProviderWaffo:        true,
	PaymentProviderWaffoPancake: true,
}

func isWalletTopUpPaymentProvider(paymentProvider string) bool {
	return walletTopUpPaymentProviders[paymentProvider]
}

func calculateWalletTopUpQuota(topUp *TopUp) (int, error) {
	if topUp == nil {
		return 0, ErrTopUpNotFound
	}
	switch topUp.PaymentProvider {
	case PaymentProviderStripe:
		return int(decimal.NewFromFloat(topUp.Money).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).IntPart()), nil
	case PaymentProviderCreem:
		return int(topUp.Amount), nil
	case PaymentProviderEpay, PaymentProviderWaffo, PaymentProviderWaffoPancake:
		return int(decimal.NewFromInt(topUp.Amount).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).IntPart()), nil
	default:
		return 0, ErrPaymentMethodMismatch
	}
}

func calculateWalletTopUpRebate(quotaToAdd int) int {
	if quotaToAdd <= 0 || common.RechargeRebateRatioForInviter <= 0 {
		return 0
	}
	return int(decimal.NewFromInt(int64(quotaToAdd)).
		Mul(decimal.NewFromFloat(common.RechargeRebateRatioForInviter)).
		Div(decimal.NewFromInt(100)).
		IntPart())
}

func topUpRebateLogMatches(existing *AffiliateLog, expected *AffiliateLog) bool {
	if existing == nil || expected == nil {
		return false
	}
	return existing.InviterId == expected.InviterId &&
		existing.InviteeId == expected.InviteeId &&
		existing.Type == expected.Type &&
		existing.TradeNo == expected.TradeNo &&
		existing.RewardQuota == expected.RewardQuota &&
		existing.BaseQuota == expected.BaseQuota &&
		math.Abs(existing.RebatePercent-expected.RebatePercent) < 0.000001
}

func createTopUpRebateLogTx(tx *gorm.DB, topUp *TopUp, log *AffiliateLog) error {
	var existing AffiliateLog
	err := lockAffiliateLogByKeyTx(tx, log.IdempotencyKey, &existing)
	if err == nil {
		if topUp != nil && topUp.Status == common.TopUpStatusSuccess && topUpRebateLogMatches(&existing, log) {
			return nil
		}
		return ErrTopUpRebateConflict
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	inserted, err := createAffiliateLogIfNotExistsTx(tx, log)
	if err != nil {
		return err
	}
	if inserted {
		return nil
	}
	if err := lockAffiliateLogByKeyTx(tx, log.IdempotencyKey, &existing); err != nil {
		return err
	}
	if topUp != nil && topUp.Status == common.TopUpStatusSuccess && topUpRebateLogMatches(&existing, log) {
		return nil
	}
	return ErrTopUpRebateConflict
}

func normalizeTopUpLookupError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrTopUpNotFound
	}
	return err
}

func creditWalletTopUpTx(tx *gorm.DB, topUp *TopUp, quotaToAdd int, expectedProvider string) (*WalletTopUpCreditResult, error) {
	if topUp == nil {
		return nil, ErrTopUpNotFound
	}
	if !isWalletTopUpPaymentProvider(expectedProvider) {
		return nil, ErrPaymentMethodMismatch
	}
	if topUp.PaymentProvider != expectedProvider {
		return nil, ErrPaymentMethodMismatch
	}
	if topUp.Status != common.TopUpStatusPending {
		return nil, ErrTopUpStatusInvalid
	}
	if quotaToAdd <= 0 {
		return nil, errors.New("invalid topup quota")
	}

	result := &WalletTopUpCreditResult{
		UserId:        topUp.UserId,
		TradeNo:       topUp.TradeNo,
		PaymentMethod: topUp.PaymentMethod,
		Provider:      topUp.PaymentProvider,
		Money:         topUp.Money,
		QuotaToAdd:    quotaToAdd,
	}

	var invitee User
	if err := tx.Select("id", "inviter_id").Where("id = ?", topUp.UserId).First(&invitee).Error; err != nil {
		return nil, err
	}

	rebateQuota := calculateWalletTopUpRebate(quotaToAdd)
	shouldGrantRebate := invitee.InviterId != 0 && invitee.InviterId != invitee.Id && rebateQuota > 0
	if shouldGrantRebate {
		var inviter User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id").
			Where("id = ?", invitee.InviterId).
			First(&inviter).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, err
			}
			shouldGrantRebate = false
		}
	}

	if shouldGrantRebate {
		rebateLog := &AffiliateLog{
			InviterId:      invitee.InviterId,
			InviteeId:      invitee.Id,
			Type:           AffiliateLogTypeTopUpRebate,
			TradeNo:        topUp.TradeNo,
			IdempotencyKey: TopUpRebateIdempotencyKey(topUp.TradeNo),
			RewardQuota:    rebateQuota,
			BaseQuota:      quotaToAdd,
			RebatePercent:  common.RechargeRebateRatioForInviter,
		}
		if err := createTopUpRebateLogTx(tx, topUp, rebateLog); err != nil {
			return nil, err
		}
	}

	topUp.CompleteTime = common.GetTimestamp()
	topUp.Status = common.TopUpStatusSuccess
	if err := tx.Save(topUp).Error; err != nil {
		return nil, err
	}

	if err := tx.Model(&User{}).Where("id = ?", topUp.UserId).Update("quota", gorm.Expr("quota + ?", quotaToAdd)).Error; err != nil {
		return nil, err
	}

	if !shouldGrantRebate {
		return result, nil
	}

	update := tx.Model(&User{}).
		Where("id = ?", invitee.InviterId).
		Updates(map[string]interface{}{
			"aff_quota":   gorm.Expr("aff_quota + ?", rebateQuota),
			"aff_history": gorm.Expr("aff_history + ?", rebateQuota),
		})
	if update.Error != nil {
		return nil, update.Error
	}
	if update.RowsAffected == 0 {
		return nil, ErrTopUpRebateConflict
	}

	result.InviterId = invitee.InviterId
	result.RebateQuota = rebateQuota
	return result, nil
}

func invalidateWalletTopUpQuotaCache(result *WalletTopUpCreditResult) {
	if result == nil {
		return
	}
	if result.UserId != 0 && result.QuotaToAdd > 0 {
		if err := invalidateUserCache(result.UserId); err != nil {
			common.SysLog("failed to invalidate user quota cache after wallet topup: " + err.Error())
		}
	}
	if result.InviterId != 0 && result.RebateQuota > 0 {
		if err := invalidateUserCache(result.InviterId); err != nil {
			common.SysLog("failed to invalidate inviter cache after wallet topup rebate: " + err.Error())
		}
	}
}

func (topUp *TopUp) Insert() error {
	var err error
	err = DB.Create(topUp).Error
	return err
}

func (topUp *TopUp) Update() error {
	var err error
	err = DB.Save(topUp).Error
	return err
}

func GetTopUpById(id int) *TopUp {
	var topUp *TopUp
	var err error
	err = DB.Where("id = ?", id).First(&topUp).Error
	if err != nil {
		return nil
	}
	return topUp
}

func GetTopUpByTradeNo(tradeNo string) *TopUp {
	var topUp *TopUp
	var err error
	err = DB.Where("trade_no = ?", tradeNo).First(&topUp).Error
	if err != nil {
		return nil
	}
	return topUp
}

func UpdatePendingTopUpStatus(tradeNo string, expectedPaymentProvider string, targetStatus string) error {
	if tradeNo == "" {
		return errors.New("payment trade number is required")
	}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		topUp := &TopUp{}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(refCol+" = ?", tradeNo).First(topUp).Error; err != nil {
			return normalizeTopUpLookupError(err)
		}
		if expectedPaymentProvider != "" && topUp.PaymentProvider != expectedPaymentProvider {
			return ErrPaymentMethodMismatch
		}
		if topUp.Status != common.TopUpStatusPending {
			return ErrTopUpStatusInvalid
		}

		topUp.Status = targetStatus
		return tx.Save(topUp).Error
	})
}

func CompleteEpayWalletTopUp(tradeNo string, actualPaymentMethod string, callerIp string) error {
	if tradeNo == "" {
		return errors.New("payment trade number is required")
	}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	var result *WalletTopUpCreditResult
	err := DB.Transaction(func(tx *gorm.DB) error {
		topUp := &TopUp{}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(refCol+" = ?", tradeNo).First(topUp).Error; err != nil {
			return normalizeTopUpLookupError(err)
		}

		if topUp.PaymentProvider != PaymentProviderEpay {
			return ErrPaymentMethodMismatch
		}
		if topUp.Status == common.TopUpStatusSuccess {
			return nil
		}
		if topUp.Status != common.TopUpStatusPending {
			return ErrTopUpStatusInvalid
		}
		if actualPaymentMethod != "" && topUp.PaymentMethod != actualPaymentMethod {
			topUp.PaymentMethod = actualPaymentMethod
		}

		quotaToAdd, err := calculateWalletTopUpQuota(topUp)
		if err != nil {
			return err
		}
		result, err = creditWalletTopUpTx(tx, topUp, quotaToAdd, PaymentProviderEpay)
		return err
	})
	if err != nil {
		return err
	}
	invalidateWalletTopUpQuotaCache(result)
	if result != nil && result.QuotaToAdd > 0 {
		RecordTopupLog(result.UserId, fmt.Sprintf("Epay wallet topup succeeded, quota: %v, paid amount: %f", logger.LogQuota(result.QuotaToAdd), result.Money), callerIp, result.PaymentMethod, PaymentProviderEpay)
	}
	return nil
}

func Recharge(referenceId string, customerId string, callerIp string) (err error) {
	if referenceId == "" {
		return errors.New("payment trade number is required")
	}

	var result *WalletTopUpCreditResult
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(refCol+" = ?", referenceId).First(topUp).Error
		if err != nil {
			return normalizeTopUpLookupError(err)
		}

		if topUp.PaymentProvider != PaymentProviderStripe {
			return ErrPaymentMethodMismatch
		}
		if topUp.Status == common.TopUpStatusSuccess {
			return nil
		}
		if topUp.Status != common.TopUpStatusPending {
			return ErrTopUpStatusInvalid
		}

		quotaToAdd, err := calculateWalletTopUpQuota(topUp)
		if err != nil {
			return err
		}
		result, err = creditWalletTopUpTx(tx, topUp, quotaToAdd, PaymentProviderStripe)
		if err != nil {
			return err
		}
		if customerId != "" {
			if err := tx.Model(&User{}).Where("id = ?", topUp.UserId).Update("stripe_customer", customerId).Error; err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		common.SysError("topup failed: " + err.Error())
		return errors.New("topup failed, please retry later")
	}
	if result == nil {
		return nil
	}
	invalidateWalletTopUpQuotaCache(result)

	RecordTopupLog(topUp.UserId, fmt.Sprintf("wallet topup succeeded, quota: %v, paid amount: %d", logger.FormatQuota(result.QuotaToAdd), topUp.Amount), callerIp, topUp.PaymentMethod, PaymentMethodStripe)

	return nil
}

// topUpQueryWindowSeconds limits top-up list queries to recent records.
const topUpQueryWindowSeconds int64 = 30 * 24 * 60 * 60

// topUpQueryCutoff returns the earliest create_time included in top-up list queries.
func topUpQueryCutoff() int64 {
	return common.GetTimestamp() - topUpQueryWindowSeconds
}

func GetUserTopUps(userId int, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	// Start transaction
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	cutoff := topUpQueryCutoff()

	// Get total count within transaction
	err = tx.Model(&TopUp{}).Where("user_id = ? AND create_time >= ?", userId, cutoff).Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Get paginated topups within same transaction
	err = tx.Where("user_id = ? AND create_time >= ?", userId, cutoff).Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Commit transaction
	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return topups, total, nil
}

// GetAllTopUps returns all top-up records with pagination.
func GetAllTopUps(pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	if err = tx.Model(&TopUp{}).Count(&total).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return topups, total, nil
}

// searchTopUpCountHardLimit caps expensive count queries.
const searchTopUpCountHardLimit = 10000

// SearchUserTopUps searches recent top-ups for one user.
func SearchUserTopUps(userId int, keyword string, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	query := tx.Model(&TopUp{}).Where("user_id = ? AND create_time >= ?", userId, topUpQueryCutoff())
	if keyword != "" {
		pattern, perr := sanitizeLikePattern(keyword)
		if perr != nil {
			tx.Rollback()
			return nil, 0, perr
		}
		query = query.Where("trade_no LIKE ? ESCAPE '!'", pattern)
	}

	if err = query.Limit(searchTopUpCountHardLimit).Count(&total).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to count search topups: " + err.Error())
		return nil, 0, errors.New("failed to query topups")
	}

	if err = query.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to search topups: " + err.Error())
		return nil, 0, errors.New("failed to query topups")
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}
	return topups, total, nil
}

// SearchAllTopUps searches all top-up records with pagination.
func SearchAllTopUps(keyword string, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	query := tx.Model(&TopUp{})
	if keyword != "" {
		pattern, perr := sanitizeLikePattern(keyword)
		if perr != nil {
			tx.Rollback()
			return nil, 0, perr
		}
		query = query.Where("trade_no LIKE ? ESCAPE '!'", pattern)
	}

	if err = query.Limit(searchTopUpCountHardLimit).Count(&total).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to count search topups: " + err.Error())
		return nil, 0, errors.New("failed to query topups")
	}

	if err = query.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to search topups: " + err.Error())
		return nil, 0, errors.New("failed to query topups")
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}
	return topups, total, nil
}

// ManualCompleteTopUp manually completes a pending top-up and credits the user.
func ManualCompleteTopUp(tradeNo string, callerIp string) error {
	if tradeNo == "" {
		return errors.New("payment trade number is required")
	}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	var result *WalletTopUpCreditResult
	err := DB.Transaction(func(tx *gorm.DB) error {
		topUp := &TopUp{}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(refCol+" = ?", tradeNo).First(topUp).Error; err != nil {
			return normalizeTopUpLookupError(err)
		}

		if topUp.Status == common.TopUpStatusSuccess {
			return nil
		}
		if topUp.Status != common.TopUpStatusPending {
			return errors.New("topup is not pending")
		}

		quotaToAdd, err := calculateWalletTopUpQuota(topUp)
		if err != nil {
			return err
		}
		result, err = creditWalletTopUpTx(tx, topUp, quotaToAdd, topUp.PaymentProvider)
		return err
	})

	if err != nil {
		return err
	}
	if result == nil {
		return nil
	}
	invalidateWalletTopUpQuotaCache(result)

	RecordTopupLog(result.UserId, fmt.Sprintf("admin completed wallet topup, quota: %v, paid amount: %f", logger.FormatQuota(result.QuotaToAdd), result.Money), callerIp, result.PaymentMethod, "admin")
	return nil
}

func RechargeCreem(referenceId string, customerEmail string, customerName string, callerIp string) (err error) {
	if referenceId == "" {
		return errors.New("payment trade number is required")
	}

	var result *WalletTopUpCreditResult
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(refCol+" = ?", referenceId).First(topUp).Error
		if err != nil {
			return normalizeTopUpLookupError(err)
		}

		if topUp.PaymentProvider != PaymentProviderCreem {
			return ErrPaymentMethodMismatch
		}
		if topUp.Status == common.TopUpStatusSuccess {
			return nil
		}
		if topUp.Status != common.TopUpStatusPending {
			return ErrTopUpStatusInvalid
		}

		quotaToAdd, err := calculateWalletTopUpQuota(topUp)
		if err != nil {
			return err
		}
		result, err = creditWalletTopUpTx(tx, topUp, quotaToAdd, PaymentProviderCreem)
		if err != nil {
			return err
		}

		if customerEmail != "" {
			var user User
			if err = tx.Where("id = ?", topUp.UserId).First(&user).Error; err != nil {
				return err
			}
			if user.Email == "" {
				if err := tx.Model(&User{}).Where("id = ?", topUp.UserId).Update("email", customerEmail).Error; err != nil {
					return err
				}
			}
		}

		return nil
	})

	if err != nil {
		common.SysError("creem topup failed: " + err.Error())
		return errors.New("topup failed, please retry later")
	}
	if result == nil {
		return nil
	}
	invalidateWalletTopUpQuotaCache(result)

	RecordTopupLog(topUp.UserId, fmt.Sprintf("Creem wallet topup succeeded, quota: %v, paid amount: %.2f", result.QuotaToAdd, topUp.Money), callerIp, topUp.PaymentMethod, PaymentMethodCreem)

	return nil
}

func RechargeWaffo(tradeNo string, callerIp string) (err error) {
	if tradeNo == "" {
		return errors.New("payment trade number is required")
	}

	var result *WalletTopUpCreditResult
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(refCol+" = ?", tradeNo).First(topUp).Error
		if err != nil {
			return normalizeTopUpLookupError(err)
		}

		if topUp.PaymentProvider != PaymentProviderWaffo {
			return ErrPaymentMethodMismatch
		}
		if topUp.Status == common.TopUpStatusSuccess {
			return nil
		}
		if topUp.Status != common.TopUpStatusPending {
			return ErrTopUpStatusInvalid
		}

		quotaToAdd, err := calculateWalletTopUpQuota(topUp)
		if err != nil {
			return err
		}
		result, err = creditWalletTopUpTx(tx, topUp, quotaToAdd, PaymentProviderWaffo)
		return err
	})

	if err != nil {
		common.SysError("waffo topup failed: " + err.Error())
		return errors.New("topup failed, please retry later")
	}
	invalidateWalletTopUpQuotaCache(result)

	if result != nil && result.QuotaToAdd > 0 {
		RecordTopupLog(topUp.UserId, fmt.Sprintf("Waffo wallet topup succeeded, quota: %v, paid amount: %.2f", logger.FormatQuota(result.QuotaToAdd), topUp.Money), callerIp, topUp.PaymentMethod, PaymentMethodWaffo)
	}

	return nil
}

func RechargeWaffoPancake(tradeNo string) (err error) {
	if tradeNo == "" {
		return errors.New("payment trade number is required")
	}

	var result *WalletTopUpCreditResult
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(refCol+" = ?", tradeNo).First(topUp).Error
		if err != nil {
			return normalizeTopUpLookupError(err)
		}

		if topUp.PaymentProvider != PaymentProviderWaffoPancake {
			return ErrPaymentMethodMismatch
		}
		if topUp.Status == common.TopUpStatusSuccess {
			return nil
		}
		if topUp.Status != common.TopUpStatusPending {
			return ErrTopUpStatusInvalid
		}

		quotaToAdd, err := calculateWalletTopUpQuota(topUp)
		if err != nil {
			return err
		}
		result, err = creditWalletTopUpTx(tx, topUp, quotaToAdd, PaymentProviderWaffoPancake)
		return err
	})

	if err != nil {
		common.SysError("waffo pancake topup failed: " + err.Error())
		return errors.New("topup failed, please retry later")
	}
	invalidateWalletTopUpQuotaCache(result)

	if result != nil && result.QuotaToAdd > 0 {
		RecordLog(topUp.UserId, LogTypeTopup, fmt.Sprintf("Waffo Pancake wallet topup succeeded, quota: %v, paid amount: %.2f", logger.FormatQuota(result.QuotaToAdd), topUp.Money))
	}

	return nil
}
