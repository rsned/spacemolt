package game

import (
	"io"
	"log"
	"sync"
	"testing"
)

func TestNewMCPClient(t *testing.T) {
	// With nil logger
	client := NewMCPClient("test-server", nil)
	if client.serverPath != "test-server" {
		t.Errorf("serverPath = %q, want %q", client.serverPath, "test-server")
	}
	if client.logger == nil {
		t.Error("logger should not be nil when passed nil")
	}

	// With custom logger
	logger := log.New(io.Discard, "", 0)
	client = NewMCPClient("test-server", logger)
	if client.logger != logger {
		t.Error("logger not set correctly")
	}
}

func TestNewMCPManager(t *testing.T) {
	// With nil logger
	mgr := NewMCPManager(nil)
	if mgr.clients == nil {
		t.Error("clients map should be initialized")
	}
	if mgr.logger == nil {
		t.Error("logger should not be nil")
	}

	// With custom logger
	logger := log.New(io.Discard, "", 0)
	mgr = NewMCPManager(logger)
	if mgr.logger != logger {
		t.Error("logger not set correctly")
	}
	if len(mgr.clients) != 0 {
		t.Error("clients should be empty initially")
	}
}

func TestMCPManager_CloseAll_Empty(t *testing.T) {
	mgr := NewMCPManager(nil)
	err := mgr.CloseAll()
	if err != nil {
		t.Errorf("CloseAll on empty manager returned error: %v", err)
	}
}

func TestMCPClient_Stop_NilProcess(t *testing.T) {
	client := NewMCPClient("test", nil)
	// Stop should not panic when cmd is nil
	err := client.Stop()
	if err != nil {
		t.Errorf("Stop on nil cmd returned error: %v", err)
	}
}

func TestMCPManager_ConcurrentAccess(t *testing.T) {
	mgr := NewMCPManager(nil)

	// Concurrent CloseAll should not race
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = mgr.CloseAll()
		}()
	}
	wg.Wait()
}
