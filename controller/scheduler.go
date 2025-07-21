package controller

// 定时任务
import (
	"one-api/common"
	"one-api/model"
	"strconv"
	"time"
)

func dailyTask() {
	common.SysLog("开始刷新令牌...")

	now, err := model.FindTokensToExecuteNow()

	//刷新余额
	if err == nil {
		for i := range now {
			common.SysLog("开始刷新令牌... 名称：" + now[i].Name + ",剩余余额：" + strconv.Itoa(now[i].RemainQuota))
			_ = now[i].RefreshTokenQuota()
		}
	}

	// 在这里执行您的指定任务
	// 比如：控制器的方法或者其他任务逻辑
	// controller.YourSpecificMethod()
	common.SysLog("Daily task completed.")
}

// StartDailyTaskScheduler 任务
// 此方法固定为 1 分钟刷新一次
func StartDailyTaskScheduler() {
	// 获取当前时间
	now := time.Now()
	// 计算距离下一次零点的时间间隔
	//next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
	//修改为每十秒执行一次
	next := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute(), now.Second()+60, 0, now.Location())

	duration := time.Until(next)
	// 使用定时器等待到零点
	go func() {
		time.Sleep(duration) // 等待
		for {
			// 执行定时任务
			dailyTask()
			// 等待 24 小时后再次执行
			//time.Sleep(24 * time.Hour)
			time.Sleep(60 * time.Second)
		}
	}()
}
