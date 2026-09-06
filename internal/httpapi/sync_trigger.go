package httpapi

import (
	"context"
	"errors"
	"log"
	"path/filepath"
	"strings"
	"time"

	"filebox/internal/store"
)

// syncTriggerDebounceDefault 是“最后变更后等待多久才执行一次触发同步”。
// syncTriggerDebounceDefault is how long the coordinator waits after the last change
// before starting one triggered sync.
const syncTriggerDebounceDefault = 30 * time.Second

// syncTriggerState 记录单个触发任务在协调器中的运行状态。
// syncTriggerState tracks one triggered task's coordinator state.
type syncTriggerState struct {
	gen     uint64
	timer   *time.Timer
	running bool
	rerun   bool
}

// StartSyncTriggers 启动触发同步协调器：加载 registry 快照，并在 ctx 取消时
// 停止所有挂起的去抖定时器。服务重启后不会补跑重启期间错过的变更（不会追溯执行）。
// StartSyncTriggers starts the triggered-sync coordinator: loads the registry snapshot
// and stops every pending debounce timer when ctx is cancelled. No retroactive run is
// performed for changes missed while the server was stopped.
func (s *Server) StartSyncTriggers(ctx context.Context) {
	s.triggerMu.Lock()
	if s.triggerStarted {
		s.triggerMu.Unlock()
		return
	}
	s.triggerCtx, s.triggerCancel = context.WithCancel(ctx)
	s.triggerStarted = true
	s.triggerMu.Unlock()

	s.refreshTriggeredTasks(ctx)
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.refreshTriggeredTasks(ctx)
			case <-ctx.Done():
				s.stopTriggerTimers()
				return
			}
		}
	}()
}

// refreshTriggeredTasks 从存储重新加载 enabled 触发任务快照：登记新增、更新既有配置，
// 移除已删除/停用/不再匹配的任务并停止其挂起定时器。
// refreshTriggeredTasks reloads the enabled triggered-task snapshot: it registers new
// tasks, refreshes existing configs, and drops removed/disabled/non-matching tasks while
// stopping their pending timers.
func (s *Server) refreshTriggeredTasks(ctx context.Context) {
	items, err := s.store.ListTriggeredSyncTasks(ctx)
	if err != nil {
		if !errors.Is(ctx.Err(), context.Canceled) {
			log.Printf("list triggered sync tasks: %v", err)
		}
		return
	}
	s.triggerMu.Lock()
	defer s.triggerMu.Unlock()
	next := make(map[int64]store.SyncTask, len(items))
	for _, item := range items {
		next[item.ID] = item
		if _, exists := s.triggerTasks[item.ID]; !exists {
			s.triggerStates[item.ID] = &syncTriggerState{}
		}
	}
	for id := range s.triggerTasks {
		if _, keep := next[id]; keep {
			continue
		}
		if state := s.triggerStates[id]; state != nil && state.timer != nil {
			state.timer.Stop()
		}
		delete(s.triggerStates, id)
		delete(s.triggerTasks, id)
	}
	s.triggerTasks = next
}

// stopTriggerTimers 停止所有挂起定时器（进程关闭路径）。
// stopTriggerTimers stops all pending timers (process shutdown path).
func (s *Server) stopTriggerTimers() {
	s.triggerMu.Lock()
	defer s.triggerMu.Unlock()
	for _, state := range s.triggerStates {
		if state.timer != nil {
			state.timer.Stop()
			state.timer = nil
		}
	}
}

// triggerSourceMatches 判断变更目录是否命中一个触发任务源目录：
// 根源（空路径）匹配一切；非根源匹配自身与自身子树内的变更。
// triggerSourceMatches reports whether a changed directory matches a triggered task's
// source: a root source ("") matches everything, otherwise the change must be the
// source itself or lie inside the source subtree.
func triggerSourceMatches(source, changedDir string) bool {
	source = strings.Trim(strings.ReplaceAll(source, "\\", "/"), "/")
	changedDir = strings.Trim(strings.ReplaceAll(changedDir, "\\", "/"), "/")
	if source == "" {
		return true
	}
	if changedDir == "" {
		return false
	}
	if changedDir == source {
		return true
	}
	return strings.HasPrefix(changedDir, source+"/")
}

// notifyFileBoxChange 通知“某个用户下的哪些目录发生了变化”。每处成功修改 FileBox
// 文件/目录的入口（上传完成、建/删/改目录、删除文件、批量删除、收集上传完成）都应调用。
// 变化会被去抖；若对应任务正在执行则合并为完成后的一次补跑。
// notifyFileBoxChange reports changed user directories to the coordinator. Every
// successful FileBox file/folder mutation entry point (upload complete, folder
// create/delete/rename, file delete, batch delete, collection upload complete) calls
// this. Changes are debounced; a change arriving during an active run coalesces into
// exactly one follow-up run after completion.
func (s *Server) notifyFileBoxChange(userID int64, changedDirs ...string) {
	s.triggerMu.Lock()
	defer s.triggerMu.Unlock()
	if !s.triggerStarted || s.triggerCtx == nil || s.triggerCtx.Err() != nil || len(s.triggerTasks) == 0 {
		return
	}
	for id, task := range s.triggerTasks {
		if task.UserID != userID {
			continue
		}
		matched := false
		for _, dir := range changedDirs {
			if triggerSourceMatches(task.SourcePath, dir) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		s.armTriggerLocked(id)
	}
}

// armTriggerLocked 安排一次去抖后的执行；任务正在运行时仅标记 rerun（完成后补跑一次）。
// armTriggerLocked schedules a debounced execution; while the task is running it only
// marks rerun (one follow-up after completion).
func (s *Server) armTriggerLocked(taskID int64) {
	state := s.triggerStates[taskID]
	if state == nil {
		state = &syncTriggerState{}
		s.triggerStates[taskID] = state
	}
	if state.running {
		state.rerun = true
		return
	}
	if state.timer != nil {
		state.timer.Stop()
	}
	state.gen++
	gen := state.gen
	state.timer = time.AfterFunc(s.triggerDebounceDuration(), func() {
		s.fireTrigger(taskID, gen)
	})
}

// triggerDebounceDuration 返回去抖时长（生产默认 30 秒，测试可缩短）。
// triggerDebounceDuration returns the debounce duration (30s in production, shortened in tests).
func (s *Server) triggerDebounceDuration() time.Duration {
	if s.triggerDebounce <= 0 {
		return syncTriggerDebounceDefault
	}
	return s.triggerDebounce
}

// fireTrigger 在去抖结束后触发执行（定时器回调）。
// fireTrigger fires the debounced run (timer callback).
func (s *Server) fireTrigger(taskID int64, gen uint64) {
	s.triggerMu.Lock()
	state := s.triggerStates[taskID]
	if state == nil || state.gen != gen {
		s.triggerMu.Unlock()
		return
	}
	state.timer = nil
	if state.running {
		state.rerun = true
		s.triggerMu.Unlock()
		return
	}
	if s.triggerCtx == nil || s.triggerCtx.Err() != nil {
		s.triggerMu.Unlock()
		return
	}
	state.running = true
	s.triggerMu.Unlock()
	go s.runTriggeredTask(taskID)
}

// runTriggeredTask 串行执行一次触发同步：等待共享任务锁（绝不并发），执行前再次核对
// 任务仍启用且满足触发条件，随后走 executeSyncTask（手动/调度共用同一执行路径）。
// runTriggeredTask runs one triggered sync serially: it waits on the shared per-task
// lock (never concurrent), re-validates the task just before executing, and delegates
// to executeSyncTask (the same path used by manual and scheduled runs).
func (s *Server) runTriggeredTask(taskID int64) {
	lock := s.syncLock(taskID)
	for {
		if lock.TryLock() {
			break
		}
		if s.triggerCtx != nil && s.triggerCtx.Err() != nil {
			s.finishTrigger(taskID)
			return
		}
		select {
		case <-time.After(250 * time.Millisecond):
		case <-s.triggerDoneChannel():
			s.finishTrigger(taskID)
			return
		}
	}
	defer lock.Unlock()
	defer s.finishTrigger(taskID)

	task, err := s.store.GetSyncTask(context.Background(), taskID, 0, true)
	if err != nil {
		return
	}
	if !task.Enabled || task.ScheduleType != "triggered" || task.Direction != "push" ||
		task.SourceType != "filebox" || task.SourceKind != "directory" {
		return
	}
	if s.triggerRunHook != nil {
		s.triggerRunHook(context.Background(), task)
		return
	}
	runCtx, cancel := context.WithTimeout(context.Background(), syncTaskTimeout)
	defer cancel()
	s.executeSyncTask(runCtx, task)
}

// triggerDoneChannel 返回协调器 ctx 取消时关闭的 channel；nil ctx 时永不关闭。
// triggerDoneChannel returns a channel closed when the coordinator ctx is cancelled.
func (s *Server) triggerDoneChannel() <-chan struct{} {
	s.triggerMu.Lock()
	defer s.triggerMu.Unlock()
	if s.triggerCtx == nil {
		return nil
	}
	return s.triggerCtx.Done()
}

// finishTrigger 标记执行结束；期间有合并请求时按去抖安排一次补跑。
// finishTrigger marks the run finished and schedules one debounced follow-up when a
// coalesced request arrived while running.
func (s *Server) finishTrigger(taskID int64) {
	s.triggerMu.Lock()
	defer s.triggerMu.Unlock()
	state := s.triggerStates[taskID]
	if state == nil {
		return
	}
	state.running = false
	if !state.rerun {
		return
	}
	state.rerun = false
	if state.timer != nil {
		state.timer.Stop()
	}
	state.gen++
	gen := state.gen
	state.timer = time.AfterFunc(s.triggerDebounceDuration(), func() {
		s.fireTrigger(taskID, gen)
	})
}

// userDirFromStorageDir 将存储目录 files/<uid>/<dir> 转为用户相对目录 <dir>。
// userDirFromStorageDir converts a storage directory (files/<uid>/<dir>) to the
// user-relative directory (<dir>) used by triggered-task source paths.
func userDirFromStorageDir(storageDir string) string {
	slash := strings.ReplaceAll(storageDir, "\\", "/")
	parts := strings.Split(slash, "/")
	for len(parts) > 0 && parts[0] == "" {
		parts = parts[1:]
	}
	if len(parts) >= 2 && parts[0] == "files" {
		parts = parts[2:]
	} else if len(parts) >= 1 && parts[0] == "files" {
		parts = parts[1:]
	}
	return strings.Join(parts, "/")
}

// userDirsFromStoragePaths 将文件级存储路径（相对 DataDir，如 files/<uid>/<dir>/<name>）
// 映射为这些文件所在目录的用户相对路径（files/<uid> 前缀剥除，去重保序）。
// userDirsFromStoragePaths maps file-level storage paths (relative to DataDir, e.g.
// files/<uid>/<dir>/<name>) to the user-relative directories that contain them
// (the files/<uid> prefix is stripped; results are deduplicated in order).
func userDirsFromStoragePaths(paths []string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		dir := userDirFromStorageDir(filepath.ToSlash(filepath.Dir(filepath.FromSlash(path))))
		if _, exists := seen[dir]; exists {
			continue
		}
		seen[dir] = struct{}{}
		result = append(result, dir)
	}
	return result
}
