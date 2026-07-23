package master

import (
	. "biliTickerStorm/internal/common"
	masterpb "biliTickerStorm/internal/master/pb"
	workerpb "biliTickerStorm/internal/worker/pb"
	"context"
	"fmt"
	"google.golang.org/grpc"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var log = GetLogger("master")

// Worker 工作节点信息
type Worker struct {
	WorkerID     string
	Address      string
	Status       WorkerStatus
	TaskAssigned string
	UpdateTime   time.Time //心跳
	BanTime      time.Time //风控时间
}

// Server 服务器结构
type Server struct {
	masterpb.UnimplementedTicketMasterServer
	workers    map[string]*Worker
	workersMux sync.RWMutex
	// 任务管理
	tasks    map[string]*TaskInfo
	tasksMux sync.RWMutex
	// 配置
	heartbeatTimeout time.Duration
	taskTimeout      time.Duration
	banTimeout       time.Duration

	maxRetries int
	// 停止信号
	stopChan        chan struct{}
	scheduleTrigger chan struct{} // 🔔 调度触发通道
}

// NewServer 创建新的服务器实例
func NewServer() *Server {
	server := &Server{
		workers:          make(map[string]*Worker),
		tasks:            make(map[string]*TaskInfo),
		heartbeatTimeout: 10 * time.Second, //
		taskTimeout:      30 * time.Second, //
		banTimeout:       5 * time.Minute,  //
		maxRetries:       3,
		stopChan:         make(chan struct{}),
		scheduleTrigger:  make(chan struct{}, 1),
	}

	go server.startHeartbeatChecker()
	go server.startTaskScheduler()
	go server.startTaskMonitor()

	return server

}

func (s *Server) LoadTasksFromDir(dirPath string) error {
	files, err := ioutil.ReadDir(dirPath)
	if err != nil {
		return err
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}
		if strings.HasSuffix(file.Name(), ".json") {
			fullPath := filepath.Join(dirPath, file.Name())
			content, err := os.ReadFile(fullPath)
			if err != nil {
				log.Printf("Failed to read file %s: %v", fullPath, err)
				continue
			}
			taskName := strings.TrimSuffix(file.Name(), ".json")
			tickerConfigContent := string(content)
			_ = s.CreateTask(taskName, tickerConfigContent)
		}
	}

	return nil
}

func (s *Server) CancelTask(ctx context.Context, req *masterpb.CancelTaskInfo) (*masterpb.CancelReply, error) {
	s.workersMux.Lock()
	s.tasksMux.Lock()
	defer s.workersMux.Unlock()
	defer s.tasksMux.Unlock()

	ownWorkerId := req.WorkerId
	worker, workerExists := s.workers[ownWorkerId]
	if !workerExists {
		return nil, fmt.Errorf("worker <%s> not found", ownWorkerId)
	}

	// 任务回 Pending，交给其他 Idle worker；允许 taskId 为空（已取消过）
	if req.CancelTaskId != "" {
		cancelTask, exists := s.tasks[req.CancelTaskId]
		if !exists {
			return nil, fmt.Errorf("<%s> not found", req.CancelTaskId)
		}
		if cancelTask.AssignedTo != "" && cancelTask.AssignedTo != req.WorkerId {
			return nil, fmt.Errorf("<%s> not own by <%s>", req.CancelTaskId, req.WorkerId)
		}
		if cancelTask.AssignedTo == req.WorkerId || cancelTask.Status == TaskStatusDoing {
			log.Printf("[Reassign] cancel task %s from %s -> PENDING", req.CancelTaskId, ownWorkerId)
			s.clearAndPendingTask(cancelTask)
		}
	}

	worker.TaskAssigned = ""
	newStatus := WorkerStatus(req.WorkStatus)
	if newStatus == Risking {
		if worker.Status != Risking {
			log.Printf("Worker %s 出现风控，标记为Risking，冷却 %.0fs", ownWorkerId, s.banTimeout.Seconds())
		}
		worker.BanTime = time.Now()
	}
	worker.Status = newStatus
	worker.UpdateTime = time.Now()
	s.triggerSchedule()

	return &masterpb.CancelReply{
		Success: true,
		Message: fmt.Sprintf("<%s> cancel <%s> Successfully.", req.WorkerId, req.CancelTaskId),
	}, nil
}

func (s *Server) RegisterWorker(ctx context.Context, req *masterpb.WorkerInfo) (*masterpb.RegisterReply, error) {
	s.workersMux.Lock()
	s.tasksMux.Lock()
	defer s.tasksMux.Unlock()
	defer s.workersMux.Unlock()
	defer s.triggerSchedule()
	existingWorker, exists := s.workers[req.WorkerId]
	if exists {
		existingWorker.Address = req.Address
		existingWorker.UpdateTime = time.Now()
		reported := WorkerStatus(req.WorkStatus)

		// 冷却期内禁止心跳把 Risking 刷成 Idle/Working
		if existingWorker.Status == Risking && reported != Risking {
			if !existingWorker.BanTime.IsZero() && time.Since(existingWorker.BanTime) < s.banTimeout {
				reported = Risking
			}
		}
		if existingWorker.Status != reported {
			if reported == Risking && existingWorker.Status != Risking {
				existingWorker.BanTime = time.Now()
				log.Printf("Worker %s 心跳上报风控，标记为Risking", req.WorkerId)
			}
			existingWorker.Status = reported
			s.triggerSchedule()
		}
		if reported == Risking {
			existingWorker.TaskAssigned = ""
		} else {
			existingWorker.TaskAssigned = req.TaskAssigned
		}

		// 仅在任务仍归属本 worker 且状态为 Doing 时接受状态推进，避免 412 后把 Pending 标成 Done
		if req.TaskAssigned != "" && reported != Risking {
			task, taskExists := s.tasks[req.TaskAssigned]
			if !taskExists {
				return nil, fmt.Errorf("<%s> not found", req.TaskAssigned)
			}
			if task.AssignedTo == req.WorkerId {
				if string(task.Status) != req.TaskStatus {
					oldStatus := task.Status
					task.Status = TaskStatus(req.TaskStatus)
					log.Printf("<%s> => <%s>: %s ", oldStatus, task.Status, task.TaskName)
					if task.Status == TaskStatusDone {
						task.AssignedTo = ""
						existingWorker.TaskAssigned = ""
					}
					s.triggerSchedule()
				}
				task.UpdatedAt = time.Now()
			}
		}
		return &masterpb.RegisterReply{
			Success: true,
			Message: "Worker Update Successfully",
		}, nil
	}
	newWorker := &Worker{
		WorkerID:     req.WorkerId,
		Address:      req.Address,
		Status:       WorkerStatus(req.WorkStatus),
		TaskAssigned: req.TaskAssigned,
		UpdateTime:   time.Now(),
	}
	s.workers[req.WorkerId] = newWorker
	log.Infof("Worker Register: ID=%s, Address=%s, WorkStatus=%s",
		req.WorkerId, req.Address, WorkerStatus(req.WorkStatus).String())
	return &masterpb.RegisterReply{
		Success: true,
		Message: "Worker Register Successfully",
	}, nil
}

// 心跳检查器
func (s *Server) startHeartbeatChecker() {
	ticker := time.NewTicker(5 * time.Second) // 每5秒检查一次
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.checkWorkerHeartbeats()
		case <-s.stopChan:
			return
		}
	}
}

func (s *Server) Stop() {
	close(s.stopChan)
	log.Println("Master Stopped")
}

func (s *Server) CreateTask(taskName, tickerConfigContent string) *TaskInfo {
	s.tasksMux.Lock()
	defer s.tasksMux.Unlock()
	defer s.triggerSchedule()

	taskID := fmt.Sprintf("task-%d", time.Now().UnixNano())
	task := &TaskInfo{
		ID:                  taskID,
		Status:              TaskStatusPending,
		CreatedAt:           time.Now(),
		UpdatedAt:           time.Now(),
		TaskName:            taskName,
		TickerConfigContent: tickerConfigContent,
	}

	s.tasks[taskID] = task
	log.Printf("Create Task : ID=%s, name=%s", taskID, taskName)
	return task
}

func (s *Server) checkWorkerHeartbeats() {
	s.workersMux.Lock()
	defer s.workersMux.Unlock()

	now := time.Now()
	offlineWorkers := make([]string, 0)
	riskingWorkers := make([]string, 0)
	workingWorkers := make([]string, 0)
	ideWorkers := make([]string, 0)

	needSchedule := false
	for workerID, worker := range s.workers {
		if now.Sub(worker.UpdateTime) > s.heartbeatTimeout {
			log.Printf("[Offline] %s timeout (%.0fs), marked as DOWN", workerID, s.heartbeatTimeout.Seconds())
			worker.Status = Down
			offlineWorkers = append(offlineWorkers, workerID)
			if worker.TaskAssigned != "" {
				log.Printf("[Reassign] %s task %s -> PENDING", workerID, worker.TaskAssigned)
				s.tasksMux.Lock()
				if task, ok := s.tasks[worker.TaskAssigned]; ok {
					s.clearAndPendingTask(task)
				}
				s.tasksMux.Unlock()
				needSchedule = true
			}
		} else if worker.Status == Risking {
			// 冷却结束 → Idle；冷却中保留节点，禁止接新任务
			if !worker.BanTime.IsZero() && now.Sub(worker.BanTime) > s.banTimeout {
				log.Printf("[Unban] %s rest time (%.0fs) ended, marked as IDLE", workerID, s.banTimeout.Seconds())
				worker.Status = Idle
				worker.BanTime = time.Time{}
				ideWorkers = append(ideWorkers, workerID)
				needSchedule = true
			} else {
				riskingWorkers = append(riskingWorkers, workerID)
			}
		} else if worker.Status == Working {
			workingWorkers = append(workingWorkers, workerID)
		} else if worker.Status == Idle {
			ideWorkers = append(ideWorkers, workerID)
		}
	}
	log.Printf("[Worker] Banned: %d, Idle: %d, Working: %d", len(riskingWorkers), len(ideWorkers), len(workingWorkers))
	for _, workerID := range offlineWorkers {
		delete(s.workers, workerID)
	}
	if needSchedule {
		s.triggerSchedule()
	}
}
func (s *Server) triggerSchedule() {
	select {
	case s.scheduleTrigger <- struct{}{}:
	default:
		// 排队跳过
	}
}

// 任务调度器
func (s *Server) startTaskScheduler() {
	for {
		select {
		case <-s.scheduleTrigger:
			s.scheduleTasks()
		case <-s.stopChan:
			return
		}
	}
}

func (s *Server) scheduleTasks() {
	s.tasksMux.Lock()
	s.workersMux.RLock()
	idleWorkers := make([]*Worker, 0)
	for _, worker := range s.workers {
		if worker.Status == Idle {
			idleWorkers = append(idleWorkers, worker)
		}
	}

	pendingTasks := make([]*TaskInfo, 0) //需要分配的task
	for _, task := range s.tasks {
		if task.Status == TaskStatusPending { //过滤一下，保证s.taskQueue 里面都是pendingTasks
			pendingTasks = append(pendingTasks, task)
		}
	}
	s.workersMux.RUnlock()
	s.tasksMux.Unlock()

	assigned := 0
	for i, task := range pendingTasks {
		if i >= len(idleWorkers) {
			break // not enough
		}
		worker := idleWorkers[i]
		if s.assignTaskToWorker(task, worker) {
			assigned++
		}
	}
}

// 整理需要重新分配的task，释放这些tasker
func (s *Server) startTaskMonitor() {
	ticker := time.NewTicker(5 * time.Second) // 每5秒检查一次
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.monitorTasks()
		case <-s.stopChan:
			return
		}
	}
}

func (s *Server) monitorTasks() {
	s.tasksMux.Lock()
	defer s.tasksMux.Unlock()

	now := time.Now()
	pendingTasks := make([]*TaskInfo, 0)
	doingTasks := make([]*TaskInfo, 0)
	doneTasks := make([]*TaskInfo, 0)

	DoneTaskNum := 0
	for _, task := range s.tasks {
		if task.Status == TaskStatusDoing {
			if now.Sub(task.UpdatedAt) > s.taskTimeout {
				log.Printf("[Timeout] Task %s timeout, marked as PENDING", task.ID)
				task.Status = TaskStatusPending
				pendingTasks = append(pendingTasks, task)
			} else {
				doingTasks = append(doingTasks, task)
			}
		} else if task.Status == TaskStatusPending {
			pendingTasks = append(pendingTasks, task)
		} else if task.Status == TaskStatusDone {
			doneTasks = append(doneTasks, task)
		}
	}
	if DoneTaskNum == len(s.tasks) {
		log.Infof("[Complete] All tasks done")
		log.Exit(0)
	}

	log.Infof("[Task] Pending: %d, Done: %d, Doing: %d", len(pendingTasks), len(doneTasks), len(doingTasks))
	// 重新分配risking任务
	if len(pendingTasks) > 0 {
		defer s.triggerSchedule()
	}
	for _, task := range pendingTasks {
		s.clearAndPendingTask(task)
	}
}

// 分配任务给worker
func (s *Server) assignTaskToWorker(task *TaskInfo, worker *Worker) bool {
	// 通过gRPC调用worker
	conn, err := grpc.Dial(worker.Address, grpc.WithInsecure())
	if err != nil {
		log.Printf("[ConnectFail] Worker %s: %v", worker.WorkerID, err)
		return false
	}
	defer conn.Close()

	client := workerpb.NewTicketWorkerClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &workerpb.TaskRequest{
		TaskId:      task.ID,
		TicketsInfo: task.TickerConfigContent,
	}

	reply, err := client.PushTask(ctx, req)
	if err != nil {
		log.Printf("[AssignFail] Worker %s: %v", worker.WorkerID, err)
		return false
	}

	if !reply.Success {
		log.Printf("[Reject] Worker %s: %s", worker.WorkerID, reply.Message)
		return false
	}

	// 更新状态
	s.tasksMux.Lock()
	task.Status = TaskStatusDoing
	task.AssignedTo = worker.WorkerID
	task.UpdatedAt = time.Now()
	s.tasksMux.Unlock()

	s.workersMux.Lock()
	worker.Status = Working
	worker.TaskAssigned = task.ID
	s.workersMux.Unlock()
	log.Printf("[Assign] Task <%s> -> Worker <%s>", task.TaskName, worker.Address)
	return true
}

// 重新分配任务
func (s *Server) clearAndPendingTask(task *TaskInfo) {
	task.RetryCount++
	task.Status = TaskStatusPending
	task.AssignedTo = ""
	task.UpdatedAt = time.Now()
}
