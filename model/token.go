package model

import (
	"errors"
	"fmt"
	"one-api/common"
	"strconv"
	"strings"
	"time"

	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
)

type Token struct {
	Id                 int            `json:"id"`
	UserId             int            `json:"user_id" gorm:"index"`
	Key                string         `json:"key" gorm:"type:char(48);"`
	Status             int            `json:"status" gorm:"default:1"`
	Name               string         `json:"name" gorm:"index" `
	CreatedTime        int64          `json:"created_time" gorm:"bigint"`
	AccessedTime       int64          `json:"accessed_time" gorm:"bigint"`
	ExpiredTime        int64          `json:"expired_time" gorm:"bigint;default:-1"` // -1 means never expired
	RemainQuota        int            `json:"remain_quota" gorm:"default:0"`
	UnlimitedQuota     bool           `json:"unlimited_quota"`
	ModelLimitsEnabled bool           `json:"model_limits_enabled"`
	ModelLimits        string         `json:"model_limits" gorm:"type:varchar(1024);default:''"`
	AllowIps           *string        `json:"allow_ips" gorm:"default:''"`
	UsedQuota          int            `json:"used_quota" gorm:"default:0"` // used quota
	Group              string         `json:"group" gorm:"default:''"`
	IntervalQuota      int            `json:"interval_quota" gorm:"bigint"`    // 刷新配额
	IntervalTime       int            `json:"interval_time" gorm:"default:0"`  //间隔时间，与间隔单位组合使用，可与token类型组合使用，如1天卡、三天卡等等
	TriggerLastTime    int64          `json:"trigger_last_time" gorm:"bigint"` //上次执行时间
	IntervalUnit       int            `json:"interval_unit" gorm:"default:3"`  //token类型，默认为天，3 天、4 周、5 月、6 季度、7年卡、8周不刷新次卡、9月不刷新次卡、10季不刷新卡
	DeletedAt          gorm.DeletedAt `gorm:"index"`
}

func (token *Token) Clean() {
	token.Key = ""
}

func (token *Token) GetIpLimitsMap() map[string]any {
	// delete empty spaces
	//split with \n
	ipLimitsMap := make(map[string]any)
	if token.AllowIps == nil {
		return ipLimitsMap
	}
	cleanIps := strings.ReplaceAll(*token.AllowIps, " ", "")
	if cleanIps == "" {
		return ipLimitsMap
	}
	ips := strings.Split(cleanIps, "\n")
	for _, ip := range ips {
		ip = strings.TrimSpace(ip)
		ip = strings.ReplaceAll(ip, ",", "")
		if common.IsIP(ip) {
			ipLimitsMap[ip] = true
		}
	}
	return ipLimitsMap
}

// RefreshTokenQuota refreshTokenQuota 刷新余额
func (token *Token) RefreshTokenQuota() (err error) {
	token.RemainQuota = token.IntervalQuota //刷新为间隔额度
	token.TriggerLastTime = common.GetTimestamp()
	err = token.Update()

	return err
}

// FindTokensToExecuteNow 查询当前需要执行的按天定时任务，且排除ExpiredTime为-1的任务
func FindTokensToExecuteNow() ([]Token, error) {
	var tokens []Token
	now := time.Now().Unix() // 当前时间戳

	// 只取天卡，expired_time!=-1，interval_time>0，且当前已经到定时任务的时间
	// 修正查询条件，正确处理interval_time=-1的情况
	err := DB.Model(&Token{}).
		Where("expired_time != ? AND expired_time > ? AND status = ?", -1, now, 1).
		Where("((interval_time > 0) OR (interval_time = -1))").
		Where("trigger_last_time + CASE WHEN interval_time = -1 THEN 86400 ELSE interval_time * 86400 END <= ?", now).
		Find(&tokens).Error

	return tokens, err
}

func GetAllUserTokens(userId int, startIdx int, num int) ([]*Token, error) {
	var tokens []*Token
	var err error
	err = DB.Where("user_id = ?", userId).Order("id desc").Limit(num).Offset(startIdx).Find(&tokens).Error
	return tokens, err
}

func SearchUserTokens(userId int, keyword string, token string) (tokens []*Token, err error) {
	if token != "" {
		token = strings.Trim(token, "sk-")
	}
	err = DB.Where("user_id = ?", userId).Where("name LIKE ?", "%"+keyword+"%").Where(commonKeyCol+" LIKE ?", "%"+token+"%").Find(&tokens).Error
	return tokens, err
}

func GetIntervalSeconds(unit int) int64 {
	switch unit {
	case 3: // 天
		return 86400
	case 4: // 周
		return 604800
	case 5: // 月（30天）
		return 2592000
	case 6: // 季度（90天）
		return 7776000
	case 7: // 年（365天）
		return 31536000 // 年（365天）
	case 8: // 周不刷新次卡
		return 604800
	case 9: // 月不刷新次卡
		return 2592000
	case 10: // 季不刷新次卡
		return 7776000
	default:
		return 86400 // 默认按天
	}
}

func GetIntervalString(unit int) string {
	switch unit {
	case 3: // 天
		return "天卡"
	case 4: // 周
		return "周卡"
	case 5: // 月（30天）
		return "月卡"
	case 6: // 季度（90天）
		return "季卡"
	case 7: // 年（365天）
		return "年卡"
	case 8: // 年（365天）
		return "周不刷新次卡"
	case 9: // 年（365天）
		return "月不刷新次卡"
	case 10: // 年（365天）
		return "季不刷新次卡"
	default:
		return "天卡" // 默认按天
	}
}

func ValidateUserToken(key string) (token *Token, err error) {
	if key == "" {
		return nil, errors.New("未提供令牌")
	}
	token, err = GetTokenByKey(key, false)
	if err == nil {
		//检验是否激活，未激活则激活token(刷新过期时间)
		if token.ExpiredTime == -1 {
			// 需要激活
			now := common.GetTimestamp()
			unit := token.IntervalUnit
			intervalSeconds := GetIntervalSeconds(unit)
			if token.IntervalTime > 0 {
				token.ExpiredTime = now + int64(token.IntervalTime)*intervalSeconds
			} else {
				// 没有设置IntervalTime,则默认为1
				token.ExpiredTime = now + 1*intervalSeconds
			}
			triggerLastTime := now
			//为不刷新卡时，上次执行时间为结束时间
			if unit == 8 || unit == 9 || unit == 10 {
				triggerLastTime = token.ExpiredTime
			}
			token.TriggerLastTime = triggerLastTime

			// 更新到数据库
			if err := token.Update(); err != nil {
				common.SysError("激活token时数据库更新失败: " + err.Error())
				return token, errors.New("激活令牌失败")
			}
			// 格式化过期时间
			expiredStr := time.Unix(token.ExpiredTime, 0).Format("2006-01-02 15:04:05")
			intervalString := GetIntervalString(token.IntervalUnit)
			tokenTime := 1
			if token.IntervalTime > 0 {
				tokenTime = token.IntervalTime
			}
			common.SysLog("激活令牌成功: " + token.Key + " 过期时间: " + expiredStr + " 卡类型：" + strconv.Itoa(tokenTime) + " " + intervalString)
		}

		if token.Status == common.TokenStatusExhausted {
			keyPrefix := key[:3]
			keySuffix := key[len(key)-3:]
			return token, errors.New("该令牌额度已用尽 TokenStatusExhausted[sk-" + keyPrefix + "***" + keySuffix + "]")
		} else if token.Status == common.TokenStatusExpired {
			return token, errors.New("该令牌已过期")
		}
		if token.Status != common.TokenStatusEnabled {
			return token, errors.New("该令牌状态不可用")
		}
		if token.ExpiredTime != -1 && token.ExpiredTime < common.GetTimestamp() {
			if !common.RedisEnabled {
				token.Status = common.TokenStatusExpired
				err := token.SelectUpdate()
				if err != nil {
					common.SysError("failed to update token status" + err.Error())
				}
			}
			return token, errors.New("该令牌已过期")
		}
		if !token.UnlimitedQuota && token.RemainQuota <= 0 {
			if !common.RedisEnabled {
				// in this case, we can make sure the token is exhausted
				token.Status = common.TokenStatusExhausted
				err := token.SelectUpdate()
				if err != nil {
					common.SysError("failed to update token status" + err.Error())
				}
			}
			keyPrefix := key[:3]
			keySuffix := key[len(key)-3:]
			return token, errors.New(fmt.Sprintf("[sk-%s***%s] 该令牌额度已用尽 !token.UnlimitedQuota && token.RemainQuota = %d", keyPrefix, keySuffix, token.RemainQuota))
		}
		return token, nil
	}
	return nil, errors.New("无效的令牌")
}

func GetTokenByIds(id int, userId int) (*Token, error) {
	if id == 0 || userId == 0 {
		return nil, errors.New("id 或 userId 为空！")
	}
	token := Token{Id: id, UserId: userId}
	var err error = nil
	err = DB.First(&token, "id = ? and user_id = ?", id, userId).Error
	return &token, err
}

func GetTokenById(id int) (*Token, error) {
	if id == 0 {
		return nil, errors.New("id 为空！")
	}
	token := Token{Id: id}
	var err error = nil
	err = DB.First(&token, "id = ?", id).Error
	if shouldUpdateRedis(true, err) {
		gopool.Go(func() {
			if err := cacheSetToken(token); err != nil {
				common.SysError("failed to update user status cache: " + err.Error())
			}
		})
	}
	return &token, err
}

func GetTokenByKey(key string, fromDB bool) (token *Token, err error) {
	defer func() {
		// Update Redis cache asynchronously on successful DB read
		if shouldUpdateRedis(fromDB, err) && token != nil {
			gopool.Go(func() {
				if err := cacheSetToken(*token); err != nil {
					common.SysError("failed to update user status cache: " + err.Error())
				}
			})
		}
	}()
	if !fromDB && common.RedisEnabled {
		// Try Redis first
		token, err := cacheGetTokenByKey(key)
		if err == nil {
			return token, nil
		}
		// Don't return error - fall through to DB
	}
	fromDB = true
	err = DB.Where(commonKeyCol+" = ?", key).First(&token).Error
	return token, err
}

func (token *Token) Insert() error {
	var err error
	err = DB.Create(token).Error
	return err
}

// Update Make sure your token's fields is completed, because this will update non-zero values
func (token *Token) Update() (err error) {
	defer func() {
		if shouldUpdateRedis(true, err) {
			gopool.Go(func() {
				err := cacheSetToken(*token)
				if err != nil {
					common.SysError("failed to update token cache: " + err.Error())
				}
			})
		}
	}()
	err = DB.Model(token).Select("name", "status", "expired_time", "remain_quota", "unlimited_quota",
		"model_limits_enabled", "model_limits", "allow_ips", "group",
		"interval_quota", "interval_time", "trigger_last_time", "interval_unit").Updates(token).Error
	return err
}

func (token *Token) SelectUpdate() (err error) {
	defer func() {
		if shouldUpdateRedis(true, err) {
			gopool.Go(func() {
				err := cacheSetToken(*token)
				if err != nil {
					common.SysError("failed to update token cache: " + err.Error())
				}
			})
		}
	}()
	// This can update zero values
	return DB.Model(token).Select("accessed_time", "status").Updates(token).Error
}

func (token *Token) Delete() (err error) {
	defer func() {
		if shouldUpdateRedis(true, err) {
			gopool.Go(func() {
				err := cacheDeleteToken(token.Key)
				if err != nil {
					common.SysError("failed to delete token cache: " + err.Error())
				}
			})
		}
	}()
	err = DB.Delete(token).Error
	return err
}

func (token *Token) IsModelLimitsEnabled() bool {
	return token.ModelLimitsEnabled
}

func (token *Token) GetModelLimits() []string {
	if token.ModelLimits == "" {
		return []string{}
	}
	return strings.Split(token.ModelLimits, ",")
}

func (token *Token) GetModelLimitsMap() map[string]bool {
	limits := token.GetModelLimits()
	limitsMap := make(map[string]bool)
	for _, limit := range limits {
		limitsMap[limit] = true
	}
	return limitsMap
}

func DisableModelLimits(tokenId int) error {
	token, err := GetTokenById(tokenId)
	if err != nil {
		return err
	}
	token.ModelLimitsEnabled = false
	token.ModelLimits = ""
	return token.Update()
}

func DeleteTokenById(id int, userId int) (err error) {
	// Why we need userId here? In case user want to delete other's token.
	if id == 0 || userId == 0 {
		return errors.New("id 或 userId 为空！")
	}
	token := Token{Id: id, UserId: userId}
	err = DB.Where(token).First(&token).Error
	if err != nil {
		return err
	}
	return token.Delete()
}

func IncreaseTokenQuota(id int, key string, quota int) (err error) {
	if quota < 0 {
		return errors.New("quota 不能为负数！")
	}
	if common.RedisEnabled {
		gopool.Go(func() {
			err := cacheIncrTokenQuota(key, int64(quota))
			if err != nil {
				common.SysError("failed to increase token quota: " + err.Error())
			}
		})
	}
	if common.BatchUpdateEnabled {
		addNewRecord(BatchUpdateTypeTokenQuota, id, quota)
		return nil
	}
	return increaseTokenQuota(id, quota)
}

func increaseTokenQuota(id int, quota int) (err error) {
	err = DB.Model(&Token{}).Where("id = ?", id).Updates(
		map[string]interface{}{
			"remain_quota":  gorm.Expr("remain_quota + ?", quota),
			"used_quota":    gorm.Expr("used_quota - ?", quota),
			"accessed_time": common.GetTimestamp(),
		},
	).Error
	return err
}

func DecreaseTokenQuota(id int, key string, quota int) (err error) {
	if quota < 0 {
		return errors.New("quota 不能为负数！")
	}
	if common.RedisEnabled {
		gopool.Go(func() {
			err := cacheDecrTokenQuota(key, int64(quota))
			if err != nil {
				common.SysError("failed to decrease token quota: " + err.Error())
			}
		})
	}
	if common.BatchUpdateEnabled {
		addNewRecord(BatchUpdateTypeTokenQuota, id, -quota)
		return nil
	}
	return decreaseTokenQuota(id, quota)
}

func decreaseTokenQuota(id int, quota int) (err error) {
	err = DB.Model(&Token{}).Where("id = ?", id).Updates(
		map[string]interface{}{
			"remain_quota":  gorm.Expr("remain_quota - ?", quota),
			"used_quota":    gorm.Expr("used_quota + ?", quota),
			"accessed_time": common.GetTimestamp(),
		},
	).Error
	return err
}

// CountUserTokens returns total number of tokens for the given user, used for pagination
func CountUserTokens(userId int) (int64, error) {
	var total int64
	err := DB.Model(&Token{}).Where("user_id = ?", userId).Count(&total).Error
	return total, err
}

// BatchDeleteTokens 删除指定用户的一组令牌，返回成功删除数量
func BatchDeleteTokens(ids []int, userId int) (int, error) {
	if len(ids) == 0 {
		return 0, errors.New("ids 不能为空！")
	}

	tx := DB.Begin()

	var tokens []Token
	if err := tx.Where("user_id = ? AND id IN (?)", userId, ids).Find(&tokens).Error; err != nil {
		tx.Rollback()
		return 0, err
	}

	if err := tx.Where("user_id = ? AND id IN (?)", userId, ids).Delete(&Token{}).Error; err != nil {
		tx.Rollback()
		return 0, err
	}

	if err := tx.Commit().Error; err != nil {
		return 0, err
	}

	if common.RedisEnabled {
		gopool.Go(func() {
			for _, t := range tokens {
				_ = cacheDeleteToken(t.Key)
			}
		})
	}

	return len(tokens), nil
}
