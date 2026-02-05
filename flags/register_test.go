package flags

import (
	"testing"
)

type Config struct {
	Name    string `json:"name" short:"n" default:"vigo"`
	Port    int    `json:"port" short:"p" default:"8080"`
	Verbose bool   `json:"verbose" short:"v"`
}

func TestAutoRegisterShort(t *testing.T) {
	cfg := &Config{}
	f := New("test", "test flags", nil)
	f.AutoRegister(cfg)

	// Verify long flags
	if f.Lookup("name") == nil {
		t.Error("flag 'name' not registered")
	}
	if f.Lookup("port") == nil {
		t.Error("flag 'port' not registered")
	}

	// Verify short flags
	if f.Lookup("n") == nil {
		t.Error("flag 'n' (short for name) not registered")
	}
	if f.Lookup("p") == nil {
		t.Error("flag 'p' (short for port) not registered")
	}
	if f.Lookup("v") == nil {
		t.Error("flag 'v' (short for verbose) not registered")
	}

    // Verify values match
    
    // Set short flag
    if err := f.Set("n", "new_name"); err != nil {
        t.Fatalf("failed to set flag 'n': %v", err)
    }
    if cfg.Name != "new_name" {
        t.Errorf("setting short flag 'n' did not update struct field. got %s, want new_name", cfg.Name)
    }

    // Set long flag
    if err := f.Set("port", "9090"); err != nil {
        t.Fatalf("failed to set flag 'port': %v", err)
    }
    if cfg.Port != 9090 {
        t.Errorf("setting long flag 'port' did not update struct field. got %d, want 9090", cfg.Port)
    }

    // Set short flag for port
    if err := f.Set("p", "9091"); err != nil {
         t.Fatalf("failed to set flag 'p': %v", err)
    }
    if cfg.Port != 9091 {
        t.Errorf("setting short flag 'p' did not update struct field. got %d, want 9091", cfg.Port)
    }
}
