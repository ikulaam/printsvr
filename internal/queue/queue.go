package queue

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"gorm.io/gorm"

	"printsvr/internal/models"
	"printsvr/internal/printer"
)

// PrintQueueManager 打印队列调度管理器
type PrintQueueManager struct {
	db           *gorm.DB
	executor     printer.PrintJobExecutor
	printerLocks map[uint]*sync.Mutex
	locksMu      sync.Mutex
	notifyCh     chan struct{}
	ctx          context.Context
	cancel       context.CancelFunc
}

var Manager *PrintQueueManager

func InitQueue(db *gorm.DB) *PrintQueueManager {
	ctx, cancel := context.WithCancel(context.Background())
	Manager = &PrintQueueManager{
		db:           db,
		executor:     printer.NewDefaultExecutor(),
		printerLocks: make(map[uint]*sync.Mutex),
		notifyCh:     make(chan struct{}, 10),
		ctx:          ctx,
		cancel:       cancel,
	}

	// 启动后台主调度循环
	go Manager.workerLoop()

	return Manager
}

func (q *PrintQueueManager) Stop() {
	if q.cancel != nil {
		q.cancel()
	}
}

// NotifyNewJob 唤醒调度器检查新加入或修改的作业
func (q *PrintQueueManager) NotifyNewJob() {
	select {
	case q.notifyCh <- struct{}{}:
	default:
	}
}

// 获取某台打印机的互斥锁（确保单台物理打印机串行作业）
func (q *PrintQueueManager) getPrinterLock(printerID uint) *sync.Mutex {
	q.locksMu.Lock()
	defer q.locksMu.Unlock()

	mu, exists := q.printerLocks[printerID]
	if !exists {
		mu = &sync.Mutex{}
		q.printerLocks[printerID] = mu
	}
	return mu
}

// 主调度循环
func (q *PrintQueueManager) workerLoop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-q.ctx.Done():
			log.Println("[Queue] 调度器平稳停止")
			return
		case <-q.notifyCh:
			q.dispatchPendingJobs()
		case <-ticker.C:
			q.dispatchPendingJobs()
		}
	}
}

// 扫描所有待打印 (pending) 的作业并尝试派发
func (q *PrintQueueManager) dispatchPendingJobs() {
	var pendingJobs []models.PrintJob

	// 按照 优先级(从高到低) -> 提交时间(从早到晚) 排序获取 pending 任务
	err := q.db.Preload("Printer").Preload("User").
		Where("status = ?", models.JobStatusPending).
		Order("priority DESC, created_at ASC").
		Limit(20).
		Find(&pendingJobs).Error

	if err != nil || len(pendingJobs) == 0 {
		return
	}

	for i := range pendingJobs {
		job := pendingJobs[i]
		printerLock := q.getPrinterLock(job.PrinterID)

		// 非阻塞尝试获取打印机锁：如果获取到了锁，说明当前打印机闲置，启动协程处理
		if printerLock.TryLock() {
			go func(currentJob models.PrintJob, mu *sync.Mutex) {
				defer mu.Unlock()
				q.processSingleJob(&currentJob)
			}(job, printerLock)
		}
	}
}

// 处理单个打印任务
func (q *PrintQueueManager) processSingleJob(job *models.PrintJob) {
	now := time.Now()
	job.Status = models.JobStatusProcessing
	job.StartedAt = &now
	job.Progress = 5
	job.ErrorMessage = ""
	q.db.Save(job)

	// 更新打印机状态为 busy
	q.db.Model(&models.Printer{}).Where("id = ?", job.PrinterID).Update("status", models.PrinterStatusBusy)

	log.Printf("[Queue] 正在开始处理打印作业 [%s] - %s", job.JobNumber, job.FileName)

	// 进度回调
	progressCallback := func(progress int) {
		job.Progress = progress
		q.db.Model(&models.PrintJob{}).Where("id = ?", job.ID).Update("progress", progress)
	}

	// 执行打印
	err := q.executor.Execute(job, progressCallback)

	finishTime := time.Now()
	if err != nil {
		job.Status = models.JobStatusFailed
		job.ErrorMessage = err.Error()
		job.Progress = 0
		log.Printf("[Queue] 作业 [%s] 打印失败: %v", job.JobNumber, err)
	} else {
		job.Status = models.JobStatusCompleted
		job.Progress = 100
		job.PrintedAt = &finishTime
		job.ErrorMessage = ""
		log.Printf("[Queue] 作业 [%s] 打印成功完成!", job.JobNumber)

		// 扣减/累计用户配额消耗
		totalPageConsumed := job.Copies * job.PageCount
		if totalPageConsumed <= 0 {
			totalPageConsumed = 1
		}
		q.db.Model(&models.User{}).Where("id = ?", job.UserID).
			UpdateColumn("used_quota", gorm.Expr("used_quota + ?", totalPageConsumed))
	}

	q.db.Save(job)

	// 打印机状态恢复为 idle
	q.db.Model(&models.Printer{}).Where("id = ?", job.PrinterID).Update("status", models.PrinterStatusIdle)

	// 唤醒下一轮排队
	q.NotifyNewJob()
}

// CancelJob 取消一个排队中的作业
func (q *PrintQueueManager) CancelJob(jobID uint, userID uint, isAdmin bool) error {
	var job models.PrintJob
	if err := q.db.First(&job, jobID).Error; err != nil {
		return fmt.Errorf("找不到指定的打印作业")
	}

	if !isAdmin && job.UserID != userID {
		return fmt.Errorf("无权取消他人的打印任务")
	}

	if job.Status != models.JobStatusPending {
		return fmt.Errorf("任务已处于 %s 状态，无法取消", job.Status)
	}

	job.Status = models.JobStatusCancelled
	job.ErrorMessage = "用户主动取消打印"
	q.db.Save(&job)
	return nil
}

// PrioritizeJob 加急置顶
func (q *PrintQueueManager) PrioritizeJob(jobID uint) error {
	var job models.PrintJob
	if err := q.db.First(&job, jobID).Error; err != nil {
		return fmt.Errorf("找不到指定的打印作业")
	}

	if job.Status != models.JobStatusPending {
		return fmt.Errorf("仅处于排队中的任务支持加急置顶")
	}

	// 赋予超高优先级
	job.Priority = 100
	q.db.Save(&job)
	q.NotifyNewJob()
	return nil
}

// RetryJob 重新排队打印
func (q *PrintQueueManager) RetryJob(jobID uint, userID uint, isAdmin bool) error {
	var job models.PrintJob
	if err := q.db.First(&job, jobID).Error; err != nil {
		return fmt.Errorf("找不到指定的打印作业")
	}

	if !isAdmin && job.UserID != userID {
		return fmt.Errorf("无权操作该打印任务")
	}

	job.Status = models.JobStatusPending
	job.Progress = 0
	job.ErrorMessage = ""
	job.StartedAt = nil
	job.PrintedAt = nil
	job.CreatedAt = time.Now()
	q.db.Save(&job)

	q.NotifyNewJob()
	return nil
}
