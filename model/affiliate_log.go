package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrAffiliateLogConflict = errors.New("affiliate log idempotency conflict")

const (
	AffiliateLogTypeInviteReward = "invite_reward"
	AffiliateLogTypeTopUpRebate  = "topup_rebate"
)

type AffiliateLog struct {
	Id                 int     `json:"id"`
	InviterId          int     `json:"inviter_id" gorm:"index:idx_affiliate_logs_inviter_created,priority:1;index:idx_affiliate_logs_inviter_type_created,priority:1"`
	InviteeId          int     `json:"invitee_id"`
	InviteeUsername    string  `json:"invitee_username" gorm:"-:all"`
	InviteeDisplayName string  `json:"invitee_display_name" gorm:"-:all"`
	Type               string  `json:"type" gorm:"type:varchar(32);not null;index:idx_affiliate_logs_inviter_type_created,priority:2"`
	TradeNo            string  `json:"trade_no" gorm:"type:varchar(255)"`
	IdempotencyKey     string  `json:"idempotency_key" gorm:"type:varchar(191);not null;uniqueIndex"`
	RewardQuota        int     `json:"reward_quota"`
	BaseQuota          int     `json:"base_quota"`
	RebatePercent      float64 `json:"rebate_percent"`
	CreatedAt          int64   `json:"created_at" gorm:"autoCreateTime;index:idx_affiliate_logs_inviter_created,priority:2;index:idx_affiliate_logs_inviter_type_created,priority:3"`
}

type AffiliateLogPage struct {
	Items    []*AffiliateLog `json:"items"`
	Total    int64           `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"page_size"`
}

func InviteRewardIdempotencyKey(inviterId int, inviteeId int) string {
	return fmt.Sprintf("%s:%d:%d", AffiliateLogTypeInviteReward, inviterId, inviteeId)
}

func TopUpRebateIdempotencyKey(tradeNo string) string {
	return fmt.Sprintf("%s:%s", AffiliateLogTypeTopUpRebate, tradeNo)
}

func IsValidAffiliateLogType(logType string) bool {
	switch logType {
	case AffiliateLogTypeInviteReward, AffiliateLogTypeTopUpRebate:
		return true
	default:
		return false
	}
}

func CreateAffiliateLogTx(tx *gorm.DB, log *AffiliateLog) error {
	_, err := createAffiliateLogIfNotExistsTx(tx, log)
	return err
}

func lockAffiliateLogByKeyTx(tx *gorm.DB, key string, out *AffiliateLog) error {
	return tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("idempotency_key = ?", key).
		First(out).Error
}

func createAffiliateLogIfNotExistsTx(tx *gorm.DB, log *AffiliateLog) (bool, error) {
	if log.IdempotencyKey == "" {
		return false, fmt.Errorf("affiliate log idempotency key is required")
	}
	if !IsValidAffiliateLogType(log.Type) {
		return false, fmt.Errorf("invalid affiliate log type: %s", log.Type)
	}
	result := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "idempotency_key"}},
		DoNothing: true,
	}).Create(log)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func GetAffiliateLogsByInviter(inviterId int, logType string, pageInfo *common.PageInfo) (*AffiliateLogPage, error) {
	if pageInfo == nil {
		pageInfo = &common.PageInfo{Page: 1, PageSize: common.ItemsPerPage}
	}
	if pageInfo.Page < 1 {
		pageInfo.Page = 1
	}
	if pageInfo.PageSize < 1 {
		pageInfo.PageSize = common.ItemsPerPage
	}
	if pageInfo.PageSize > 100 {
		pageInfo.PageSize = 100
	}
	if logType != "" && !IsValidAffiliateLogType(logType) {
		return nil, fmt.Errorf("invalid affiliate log type: %s", logType)
	}

	query := DB.Model(&AffiliateLog{}).Where("inviter_id = ?", inviterId)
	if logType != "" {
		query = query.Where("type = ?", logType)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}

	var logs []*AffiliateLog
	if err := query.Order("created_at desc, id desc").
		Limit(pageInfo.GetPageSize()).
		Offset(pageInfo.GetStartIdx()).
		Find(&logs).Error; err != nil {
		return nil, err
	}
	if err := loadAffiliateLogInvitees(logs); err != nil {
		return nil, err
	}

	return &AffiliateLogPage{
		Items:    logs,
		Total:    total,
		Page:     pageInfo.GetPage(),
		PageSize: pageInfo.GetPageSize(),
	}, nil
}

func loadAffiliateLogInvitees(logs []*AffiliateLog) error {
	inviteeIds := make([]int, 0, len(logs))
	seen := make(map[int]struct{}, len(logs))
	for _, log := range logs {
		if log.InviteeId == 0 {
			continue
		}
		if _, ok := seen[log.InviteeId]; ok {
			continue
		}
		seen[log.InviteeId] = struct{}{}
		inviteeIds = append(inviteeIds, log.InviteeId)
	}
	if len(inviteeIds) == 0 {
		return nil
	}

	var invitees []User
	if err := DB.Select("id", "username", "display_name").Where("id IN ?", inviteeIds).Find(&invitees).Error; err != nil {
		return err
	}
	inviteeById := make(map[int]User, len(invitees))
	for _, invitee := range invitees {
		inviteeById[invitee.Id] = invitee
	}
	for _, log := range logs {
		invitee, ok := inviteeById[log.InviteeId]
		if !ok {
			continue
		}
		log.InviteeUsername = invitee.Username
		log.InviteeDisplayName = invitee.DisplayName
		if log.InviteeDisplayName == "" {
			log.InviteeDisplayName = invitee.Username
		}
	}
	return nil
}
