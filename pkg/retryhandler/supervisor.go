/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package retryhandler

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/sielicki/slingshot-operator/pkg/deviceplugin"
)

const (
	defaultCXIRHPath     = "/usr/bin/cxi_rh"
	defaultHealthPort    = 8080
	restartBackoffMin    = 1 * time.Second
	restartBackoffMax    = 60 * time.Second
	restartBackoffFactor = 2.0
)

type HandlerStatus struct {
	Device       string
	Running      bool
	PID          int
	RestartCount int
	LastError    string
	StartedAt    time.Time
}

type Supervisor struct {
	cxiRHPath  string
	healthPort int
	log        logr.Logger
	handlers   map[string]*handlerProcess
	handlersMu sync.RWMutex
	stopCh     chan struct{}
	wg         sync.WaitGroup
}

type handlerProcess struct {
	device       string
	cmd          *exec.Cmd
	cancel       context.CancelFunc
	restartCount int
	lastError    string
	startedAt    time.Time
	running      bool
	mu           sync.Mutex
}

func NewSupervisor(cxiRHPath string, healthPort int) *Supervisor {
	if cxiRHPath == "" {
		cxiRHPath = defaultCXIRHPath
	}
	if healthPort == 0 {
		healthPort = defaultHealthPort
	}

	return &Supervisor{
		cxiRHPath:  cxiRHPath,
		healthPort: healthPort,
		log:        log.Log.WithName("retry-handler"),
		handlers:   make(map[string]*handlerProcess),
		stopCh:     make(chan struct{}),
	}
}

func (s *Supervisor) Start(ctx context.Context) error {
	devices, err := deviceplugin.DiscoverDevices()
	if err != nil {
		return fmt.Errorf("failed to discover devices: %w", err)
	}

	if len(devices) == 0 {
		s.log.Info("No CXI devices found")
		return nil
	}

	s.log.Info("Starting retry handlers", "deviceCount", len(devices))

	for _, device := range devices {
		s.startHandler(ctx, device.Name)
	}

	s.wg.Add(1)
	go s.runHealthServer()

	return nil
}

func (s *Supervisor) Stop() {
	close(s.stopCh)

	s.handlersMu.Lock()
	for _, h := range s.handlers {
		if h.cancel != nil {
			h.cancel()
		}
	}
	s.handlersMu.Unlock()

	s.wg.Wait()
}

func (s *Supervisor) startHandler(ctx context.Context, deviceName string) {
	s.handlersMu.Lock()
	defer s.handlersMu.Unlock()

	if _, exists := s.handlers[deviceName]; exists {
		return
	}

	h := &handlerProcess{
		device: deviceName,
	}
	s.handlers[deviceName] = h

	s.wg.Add(1)
	go s.runHandler(ctx, h)
}

func (s *Supervisor) runHandler(ctx context.Context, h *handlerProcess) {
	defer s.wg.Done()

	backoff := restartBackoffMin

	for {
		select {
		case <-s.stopCh:
			return
		default:
		}

		runCtx, cancel := context.WithCancel(ctx)
		h.mu.Lock()
		h.cancel = cancel
		h.mu.Unlock()

		err := s.runCXIRH(runCtx, h)

		h.mu.Lock()
		h.running = false
		if err != nil {
			h.lastError = err.Error()
			h.restartCount++
			s.log.Error(err, "cxi_rh exited with error", "device", h.device, "restartCount", h.restartCount)
		}
		h.mu.Unlock()

		select {
		case <-s.stopCh:
			cancel()
			return
		default:
		}

		s.log.Info("Waiting before restart", "backoff", backoff, "device", h.device)
		select {
		case <-time.After(backoff):
		case <-s.stopCh:
			cancel()
			return
		}

		backoff = time.Duration(float64(backoff) * restartBackoffFactor)
		if backoff > restartBackoffMax {
			backoff = restartBackoffMax
		}
	}
}

func (s *Supervisor) runCXIRH(ctx context.Context, h *handlerProcess) error {
	devicePath := fmt.Sprintf("/dev/%s", h.device)

	if _, err := os.Stat(s.cxiRHPath); os.IsNotExist(err) {
		return fmt.Errorf("cxi_rh binary not found at %s", s.cxiRHPath)
	}

	cmd := exec.CommandContext(ctx, s.cxiRHPath, "-d", devicePath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	h.mu.Lock()
	h.cmd = cmd
	h.startedAt = time.Now()
	h.mu.Unlock()

	s.log.Info("Starting cxi_rh", "device", h.device, "path", s.cxiRHPath, "devicePath", devicePath)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start cxi_rh: %w", err)
	}

	h.mu.Lock()
	h.running = true
	h.mu.Unlock()

	return cmd.Wait()
}

func (s *Supervisor) GetStatus() []HandlerStatus {
	s.handlersMu.RLock()
	defer s.handlersMu.RUnlock()

	var statuses []HandlerStatus
	for _, h := range s.handlers {
		h.mu.Lock()
		status := HandlerStatus{
			Device:       h.device,
			Running:      h.running,
			RestartCount: h.restartCount,
			LastError:    h.lastError,
			StartedAt:    h.startedAt,
		}
		if h.cmd != nil && h.cmd.Process != nil {
			status.PID = h.cmd.Process.Pid
		}
		h.mu.Unlock()
		statuses = append(statuses, status)
	}
	return statuses
}

func (s *Supervisor) IsHealthy() bool {
	s.handlersMu.RLock()
	defer s.handlersMu.RUnlock()

	if len(s.handlers) == 0 {
		return true
	}

	for _, h := range s.handlers {
		h.mu.Lock()
		running := h.running
		h.mu.Unlock()
		if !running {
			return false
		}
	}
	return true
}

func (s *Supervisor) runHealthServer() {
	defer s.wg.Done()

	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if s.IsHealthy() {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintln(w, "ok")
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintln(w, "unhealthy")
		}
	})

	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ready")
	})

	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		statuses := s.GetStatus()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, "{\n  \"handlers\": [\n")
		for i, status := range statuses {
			fmt.Fprintf(w, "    {\"device\": %q, \"running\": %v, \"pid\": %d, \"restarts\": %d}",
				status.Device, status.Running, status.PID, status.RestartCount)
			if i < len(statuses)-1 {
				fmt.Fprint(w, ",")
			}
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "  ],\n  \"healthy\": %v\n}\n", s.IsHealthy())
	})

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", s.healthPort),
		Handler: mux,
	}

	go func() {
		<-s.stopCh
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	}()

	s.log.Info("Health server listening", "port", s.healthPort)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		s.log.Error(err, "Health server error")
	}
}
