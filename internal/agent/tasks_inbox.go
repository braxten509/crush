package agent

import (
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ponytail: polls the request dir; fsnotify if the latency ever matters.
func (h *taskHub) watch() {
	root := h.root
	if root == nil {
		var err error
		root, err = os.OpenRoot(h.dir)
		if err != nil {
			return
		}
	}
	defer root.Close()
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for range tick.C {
		if _, err := os.Lstat(h.dir); errors.Is(err, os.ErrNotExist) {
			return
		}
		directory, err := root.Open(".")
		if err != nil {
			return
		}
		entries, err := directory.ReadDir(-1)
		_ = directory.Close()
		if err != nil {
			continue
		}
		for _, e := range entries {
			name, ok := strings.CutSuffix(e.Name(), ".req")
			if !ok {
				continue
			}
			if !e.Type().IsRegular() {
				continue
			}
			data, err := root.ReadFile(e.Name())
			_ = root.Remove(e.Name())
			if err != nil {
				continue
			}
			// Permission prompts for background jobs must not block questions,
			// stop requests, or other jobs from reaching the hub.
			var request TaskRequest
			if json.Unmarshal(data, &request) == nil && request.Background != nil {
				go func() {
					reply := h.handle(data)
					out, _ := json.Marshal(reply)
					_ = writeTaskReply(root, name, out)
				}()
				continue
			}
			reply := h.handle(data)
			out, _ := json.Marshal(reply)
			_ = writeTaskReply(root, name, out)
		}
	}
}

func writeTaskReply(root *os.Root, name string, out []byte) error {
	tmp := ".reply-" + uuid.NewString()
	file, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer root.Remove(tmp)
	_, err = file.Write(out)
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return root.Rename(tmp, name+".ack")
}

func removeStaleTaskDirectories(base string) {
	root, err := os.OpenRoot(base)
	if err != nil {
		return
	}
	defer root.Close()
	entries, err := os.ReadDir(base)
	if err != nil {
		return
	}
	for _, entry := range entries {
		suffix, ok := strings.CutPrefix(entry.Name(), "tasks-")
		owner, random, okPID := strings.Cut(suffix, "-")
		pid, err := strconv.Atoi(owner)
		if !ok || !okPID || random == "" || err != nil || pid <= 0 || !entry.IsDir() || taskProcessAlive(pid) {
			continue
		}
		info, err := entry.Info()
		if err == nil && privateTaskDirectory(info) {
			_ = root.RemoveAll(entry.Name())
		}
	}
}

func (h *taskHub) handle(data []byte) TaskReply {
	var req TaskRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return TaskReply{Error: "bad request: " + err.Error()}
	}
	if req.Background != nil {
		return h.background(req)
	}
	if req.Skill != nil {
		message, err := h.chatSkill(req)
		if err != nil {
			return TaskReply{Error: err.Error()}
		}
		return TaskReply{Message: message}
	}
	if req.SecureEntry != nil {
		// `crush secure-entry` is shorthand for a one-question form, so the
		// secret is typed into the same inline form as every other question.
		ask, err := secureEntryAsk(*req.SecureEntry)
		if err != nil {
			return TaskReply{Error: err.Error()}
		}
		req.Ask, req.SecureEntry = ask, nil
	}
	if req.Ask != nil {
		if err := h.ask(req); err != nil {
			return TaskReply{Error: err.Error()}
		}
		return TaskReply{}
	}
	var t *Task
	var err error
	if req.Stop != "" {
		t, err = h.stop(req.Session, req.Stop)
	} else if req.Continue != "" {
		t, err = h.continueTask(req)
	} else {
		t, err = h.spawn(req)
	}
	if err != nil {
		return TaskReply{Error: err.Error()}
	}
	return TaskReply{Task: t}
}
