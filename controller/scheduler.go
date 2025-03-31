package controller

// 定时任务
import (
	"one-api/common"
	"time"
)

func dailyTask() {
	common.SysLog("Starting daily task execution...")
	// 在这里执行您的指定任务
	// 比如：控制器的方法或者其他任务逻辑
	// controller.YourSpecificMethod()
	common.SysLog("Daily task completed.")
}

// StartDailyTaskScheduler 定时刷新令牌
func StartDailyTaskScheduler() {
	// 获取当前时间
	now := time.Now()
	// 计算距离下一次零点的时间间隔
	//next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
	//修改为每十秒执行一次
	next := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute(), now.Second()+10, 0, now.Location())

	duration := time.Until(next)
	// 使用定时器等待到零点
	go func() {
		time.Sleep(duration) // 等待到零点
		for {
			// 执行定时任务
			dailyTask()
			// 等待 24 小时后再次执行
			//time.Sleep(24 * time.Hour)
			time.Sleep(10 * time.Second)
		}
	}()
}
